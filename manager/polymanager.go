/*
* Copyright (C) 2020 The poly network Authors
* This file is part of The poly network library.
*
* The poly network is free software: you can redistribute it and/or modify
* it under the terms of the GNU Lesser General Public License as published by
* the Free Software Foundation, either version 3 of the License, or
* (at your option) any later version.
*
* The poly network is distributed in the hope that it will be useful,
* but WITHOUT ANY WARRANTY; without even the implied warranty of
* MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
* GNU Lesser General Public License for more details.
* You should have received a copy of the GNU Lesser General Public License
* along with The poly network . If not, see <http://www.gnu.org/licenses/>.
 */
package manager

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ontio/ontology-crypto/keypair"
	"github.com/ontio/ontology-crypto/signature"
	"github.com/polynetwork/eth-contracts/go_abi/eccd_abi"
	"github.com/polynetwork/eth-contracts/go_abi/eccm_abi"
	"github.com/polynetwork/eth_relayer/config"
	"github.com/polynetwork/eth_relayer/db"
	"github.com/polynetwork/eth_relayer/log"
	"github.com/polynetwork/eth_relayer/rest/http/restful"
	sdk "github.com/polynetwork/poly-go-sdk"
	"github.com/polynetwork/poly/common"
	"github.com/polynetwork/poly/common/password"
	"github.com/polynetwork/poly/consensus/vbft/config"
	common2 "github.com/polynetwork/poly/native/service/cross_chain_manager/common"
	"math/rand"
	"strconv"
	"strings"

	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/polynetwork/eth_relayer/tools"

	polytypes "github.com/polynetwork/poly/core/types"
)

type PolyManager struct {
	config        *config.ServiceConfig
	polySdk       *sdk.PolySdk
	currentHeight uint32
	contractAbi   *abi.ABI
	exitChan      chan int
	db            *db.BoltDB
	ethClient     *ethclient.Client
	senders       []*EthSender
}

func NewPolyManager(servCfg *config.ServiceConfig, startblockHeight uint32, polySdk *sdk.PolySdk, ethereumsdk *ethclient.Client, boltDB *db.BoltDB) (*PolyManager, error) {
	contractabi, err := abi.JSON(strings.NewReader(eccm_abi.EthCrossChainManagerABI))
	if err != nil {
		return nil, err
	}
	chainId, err := ethereumsdk.ChainID(context.Background())
	if err != nil {
		return nil, err
	}
	ks := tools.NewEthKeyStore(servCfg.ETHConfig, chainId)
	accArr := ks.GetAccounts()
	if len(servCfg.ETHConfig.KeyStorePwdSet) == 0 {
		fmt.Println("please input the passwords for ethereum keystore: ")
		for _, v := range accArr {
			fmt.Printf("For address %s. ", v.Address.String())
			raw, err := password.GetPassword()
			if err != nil {
				log.Fatalf("failed to input password: %v", err)
				panic(err)
			}
			servCfg.ETHConfig.KeyStorePwdSet[strings.ToLower(v.Address.String())] = string(raw)
		}
	}

	senders := make([]*EthSender, len(accArr))
	for i, v := range senders {
		v = &EthSender{}
		v.acc = accArr[i]
		pwd, ok := servCfg.ETHConfig.KeyStorePwdSet[strings.ToLower(v.acc.Address.String())]
		if !ok {
			fmt.Printf("Password for address %s is not found in configuration, please input ", v.acc.Address.String())
			raw, err := password.GetPassword()
			if err != nil {
				log.Fatalf("failed to input password: %v", err)
				panic(err)
			}
			pwd = string(raw)
		}

		if err := ks.TestPwd(v.acc, pwd); err != nil {
			log.Fatalf("your password %s for account %s is not working: %v", pwd, v.acc.Address.String(), err)
			panic(err)
		}

		v.ethClient = ethereumsdk
		v.keyStore = ks
		v.pwd = pwd
		v.config = servCfg
		v.polySdk = polySdk
		v.contractAbi = &contractabi
		v.nonceManager = tools.NewNonceManager(ethereumsdk)
		v.cmap = make(map[string]chan *EthTxInfo)

		senders[i] = v
	}
	return &PolyManager{
		exitChan:      make(chan int),
		config:        servCfg,
		polySdk:       polySdk,
		currentHeight: startblockHeight,
		contractAbi:   &contractabi,
		db:            boltDB,
		ethClient:     ethereumsdk,
		senders:       senders,
	}, nil
}

func (this *PolyManager) findLatestHeight() uint32 {
	address := ethcommon.HexToAddress(this.config.ETHConfig.ECCDContractAddress)
	instance, err := eccd_abi.NewEthCrossChainData(address, this.ethClient)
	if err != nil {
		log.Errorf("findLatestHeight - new eth cross chain failed: %s", err.Error())
		return 0
	}
	height, err := instance.GetCurEpochStartHeight(nil)
	if err != nil {
		log.Errorf("findLatestHeight - GetLatestHeight failed: %s", err.Error())
		return 0
	}
	return uint32(height)
}

func (this *PolyManager) init() bool {
	if this.currentHeight > 0 {
		log.Infof("PolyManager init - start height from flag: %d", this.currentHeight)
		return true
	}
	this.currentHeight = this.db.GetPolyHeight()
	latestHeight := this.findLatestHeight()
	if latestHeight > this.currentHeight {
		this.currentHeight = latestHeight
		log.Infof("PolyManager init - latest height from ECCM: %d", this.currentHeight)
		return true
	}
	log.Infof("PolyManager init - latest height from DB: %d", this.currentHeight)

	return true
}

func (this *PolyManager) MonitorChain() {
	ret := this.init()
	if ret == false {
		log.Errorf("MonitorChain - init failed\n")
	}
	monitorTicker := time.NewTicker(config.ONT_MONITOR_INTERVAL)
	var blockHandleResult bool
	for {
		select {
		case <-monitorTicker.C:
			latestheight, err := this.polySdk.GetCurrentBlockHeight()
			if err != nil {
				log.Errorf("MonitorChain - get poly chain block height error: %s", err)
				continue
			}
			latestheight--
			if latestheight-this.currentHeight < config.ONT_USEFUL_BLOCK_NUM {
				continue
			}
			log.Infof("MonitorChain - poly chain current height: %d", latestheight)
			blockHandleResult = true
			for this.currentHeight <= latestheight-config.ONT_USEFUL_BLOCK_NUM {
				blockHandleResult = this.handleDepositEvents(this.currentHeight)
				if blockHandleResult == false {
					break
				}
				this.currentHeight++
			}
			if err = this.db.UpdatePolyHeight(this.currentHeight - 1); err != nil {
				log.Errorf("MonitorChain - failed to save height of poly: %v", err)
			}
		case <-this.exitChan:
			return
		}
	}
}

func (this *PolyManager) handleDepositEvents(height uint32) bool {
	lastEpoch := this.findLatestHeight()
	hdr, err := this.polySdk.GetHeaderByHeight(height + 1)
	if err != nil {
		log.Errorf("handleBlockHeader - GetNodeHeader on height :%d failed", height)
		return false
	}
	isCurr := lastEpoch < height+1
	isEpoch := hdr.NextBookkeeper != common.ADDRESS_EMPTY
	var (
		anchor     *polytypes.Header
		tempHdr    *polytypes.Header
		hp         string
		sigs       []byte
		headerData []byte
		rawAnchor  []byte
		timeStart  time.Time
	)
	if !isCurr {
		anchor, _ = this.polySdk.GetHeaderByHeight(lastEpoch + 1)
		proof, _ := this.polySdk.GetMerkleProof(height+1, lastEpoch+1)
		hp = proof.AuditPath
	} else if isEpoch {
		anchor, _ = this.polySdk.GetHeaderByHeight(height + 2)
		proof, _ := this.polySdk.GetMerkleProof(height+1, height+2)
		hp = proof.AuditPath
	}
	if anchor != nil && hp != "" {
		rawAnchor = anchor.GetMessage()
		tempHdr = anchor
	} else {
		tempHdr = hdr
	}
	for _, sig := range tempHdr.SigData {
		temp := make([]byte, len(sig))
		copy(temp, sig)
		newsig, _ := signature.ConvertToEthCompatible(temp)
		sigs = append(sigs, newsig...)
	}
	headerData = hdr.GetMessage()
	rawProof, _ := hex.DecodeString(hp)

	eccdAddr := ethcommon.HexToAddress(this.config.ETHConfig.ECCDContractAddress)
	eccd, err := eccd_abi.NewEthCrossChainData(eccdAddr, this.ethClient)
	if err != nil {
		panic(fmt.Errorf("failed to new eccm: %v", err))
	}
	cnt := 0
	events, err := this.polySdk.GetSmartContractEventByBlock(height)
	for err != nil {
		log.Errorf("handleDepositEvents - get block event at height:%d error: %s", height, err.Error())
		return false
	}
	for _, event := range events {
		for _, notify := range event.Notify {
			if notify.ContractAddress != this.config.PolyConfig.EntranceContractAddress {
				continue
			}
			states := notify.States.([]interface{})
			method, _ := states[0].(string)
			if method != "makeProof" {
				continue
			}
			tchainid := uint32(states[2].(float64))
			if tchainid != 2 {
				continue
			}
			proof, err := this.polySdk.GetCrossStatesProof(hdr.Height-1, states[5].(string))
			if err != nil {
				log.Errorf("handleDepositEvents - failed to get proof for key %s: %v", states[5].(string), err)
				continue
			}
			auditpath, _ := hex.DecodeString(proof.AuditPath)
			value, _, _, _ := tools.ParseAuditpath(auditpath)
			param := &common2.ToMerkleValue{}
			if err := param.Deserialization(common.NewZeroCopySource(value)); err != nil {
				log.Errorf("handleDepositEvents - failed to deserialize MakeTxParam (value: %x, err: %v)", value, err)
				continue
			}
			var isTarget bool
			contractSet, ok := this.config.TargetContracts[strconv.FormatUint(param.MakeTxParam.ToChainID, 10)]
			if ok {
				toContractStr := ethcommon.BytesToAddress(param.MakeTxParam.ToContractAddress).String()
				for _, v := range contractSet {
					if toContractStr == v {
						isTarget = true
						break
					}
				}
			}
			if !isTarget {
				continue
			}

			fromTx := [32]byte{}
			copy(fromTx[:], param.TxHash[:32])
			res, _ := eccd.CheckIfFromChainTxExist(nil, param.FromChainID, fromTx)
			if res {
				log.Debugf("handleDepositEvents - already relayed to eth: ( from_chain_id: %d, from_txhash: %x,  param.Txhash: %x)",
					param.FromChainID, param.TxHash, param.MakeTxParam.TxHash)
				continue
			}
			log.Infof("handleDepositEvents - catch poly proof with header, height: %d, key: %s, proof: %s", hdr.Height-1, states[5].(string), proof.AuditPath)

			txData, err := this.contractAbi.Pack("verifyHeaderAndExecuteTx", auditpath, headerData, rawProof, rawAnchor, sigs)
			if err != nil {
				log.Errorf("handleDepositEvents - err:" + err.Error())
				return false
			}

			gasPrice, err := this.ethClient.SuggestGasPrice(context.Background())
			if err != nil {
				log.Errorf("handleDepositEvents -  get suggest sas price failed error: %s", err.Error())
				return false
			}
			contractaddr := ethcommon.HexToAddress(this.config.ETHConfig.ECCMContractAddress)
			callMsg := ethereum.CallMsg{
				From: ethcommon.Address{}, To: &contractaddr, Gas: 0, GasPrice: gasPrice,
				Value: big.NewInt(0), Data: txData,
			}
			if _, err = this.ethClient.EstimateGas(context.Background(), callMsg); err != nil {
				log.Errorf("handleDepositEvents - estimate gas limit error: %s", err.Error())
				return false
			}
			args := &TxArgs{}
			if err := args.Deserialization(common.NewZeroCopySource(param.MakeTxParam.Args)); err != nil {
				log.Errorf("handleDepositEvents - failed to deserialize tx args: %v", err)
				return false
			}
			timeStart = time.Now()
		RETRY:
			if err := restful.FlamCli.SendEthInfo(
				param.MakeTxParam.ToChainID,
				args.amt.Uint64(),
				ethcommon.BytesToAddress(param.MakeTxParam.ToContractAddress).String(),
				"verifyHeaderAndExecuteTx",
				hex.EncodeToString(args.toAddr),
				hex.EncodeToString(args.toAssetHash),
				hex.EncodeToString(param.MakeTxParam.TxHash),
				[]string{
					proof.AuditPath, hex.EncodeToString(headerData), hp, hex.EncodeToString(rawAnchor), hex.EncodeToString(sigs),
				}); err != nil {
				if time.Now().Sub(timeStart) > this.config.RetryTimeout*time.Hour {
					log.Errorf("handleDepositEvents - retry timeout and failed to send ( poly_hash: %s ) to flamingo: %v",
						event.TxHash, err)
					continue
				}
				log.Debugf("handleDepositEvents - failed to send ( poly_hash: %s ) to flamingo and retry now: %s",
					event.TxHash, strings.Split(err.Error(), ",")[0])
				time.Sleep(time.Second * this.config.RetryDuration)
				goto RETRY
			}
			log.Infof("handleDepositEvents -  send eth info to flamingo: ( poly_tx: %s )", event.TxHash)
			// TODO: save origin tx to DB
			cnt++
		}
	}
	if cnt == 0 && isEpoch && isCurr {
		sender := this.selectSender()
		return sender.commitHeader(hdr)
	}

	return true
}

func (this *PolyManager) selectSender() *EthSender {
	sum := big.NewInt(0)
	balArr := make([]*big.Int, len(this.senders))
	for i, v := range this.senders {
	RETRY:
		bal, err := v.Balance()
		if err != nil {
			log.Errorf("failed to get balance for %s: %v", v.acc.Address.String(), err)
			time.Sleep(time.Second)
			goto RETRY
		}
		sum.Add(sum, bal)
		balArr[i] = big.NewInt(sum.Int64())
	}
	sum.Rand(rand.New(rand.NewSource(time.Now().Unix())), sum)
	for i, v := range balArr {
		res := v.Cmp(sum)
		if res == 1 || res == 0 {
			return this.senders[i]
		}
	}
	return this.senders[0]
}

func (this *PolyManager) Stop() {
	this.exitChan <- 1
	close(this.exitChan)
	log.Infof("poly chain manager exit.")
}

type EthSender struct {
	pwd          string
	acc          accounts.Account
	keyStore     *tools.EthKeyStore
	cmap         map[string]chan *EthTxInfo
	nonceManager *tools.NonceManager
	ethClient    *ethclient.Client
	polySdk      *sdk.PolySdk
	config       *config.ServiceConfig
	contractAbi  *abi.ABI
}

func (this *EthSender) commitHeader(header *polytypes.Header) bool {
	headerdata := header.GetMessage()
	var (
		txData      []byte
		txErr       error
		bookkeepers []keypair.PublicKey
		sigs        []byte
	)
	gasPrice, err := this.ethClient.SuggestGasPrice(context.Background())
	if err != nil {
		log.Errorf("commitHeader - get suggest sas price failed error: %s", err.Error())
		return false
	}
	for _, sig := range header.SigData {
		temp := make([]byte, len(sig))
		copy(temp, sig)
		newsig, _ := signature.ConvertToEthCompatible(temp)
		sigs = append(sigs, newsig...)
	}

	blkInfo := &vconfig.VbftBlockInfo{}
	if err := json.Unmarshal(header.ConsensusPayload, blkInfo); err != nil {
		log.Errorf("commitHeader - unmarshal blockInfo error: %s", err)
		return false
	}

	for _, peer := range blkInfo.NewChainConfig.Peers {
		keystr, _ := hex.DecodeString(peer.ID)
		key, _ := keypair.DeserializePublicKey(keystr)
		bookkeepers = append(bookkeepers, key)
	}
	bookkeepers = keypair.SortPublicKeys(bookkeepers)
	publickeys := make([]byte, 0)
	for _, key := range bookkeepers {
		publickeys = append(publickeys, tools.GetNoCompresskey(key)...)
	}
	txData, txErr = this.contractAbi.Pack("changeBookKeeper", headerdata, publickeys, sigs)
	if txErr != nil {
		log.Errorf("commitHeader - err:" + err.Error())
		return false
	}

	contractaddr := ethcommon.HexToAddress(this.config.ETHConfig.ECCMContractAddress)
	callMsg := ethereum.CallMsg{
		From: this.acc.Address, To: &contractaddr, Gas: 0, GasPrice: gasPrice,
		Value: big.NewInt(0), Data: txData,
	}

	gasLimit, err := this.ethClient.EstimateGas(context.Background(), callMsg)
	if err != nil {
		log.Errorf("commitHeader - estimate gas limit error: %s", err.Error())
		return false
	}

	nonce := this.nonceManager.GetAddressNonce(this.acc.Address)
	tx := types.NewTransaction(nonce, contractaddr, big.NewInt(0), gasLimit, gasPrice, txData)
	signedtx, err := this.keyStore.SignTransaction(tx, this.acc, this.pwd)
	if err != nil {
		log.Errorf("commitHeader - sign raw tx error: %s", err.Error())
		return false
	}
	if err = this.ethClient.SendTransaction(context.Background(), signedtx); err != nil {
		log.Errorf("commitHeader - send transaction error:%s\n", err.Error())
		return false
	}

	hash := header.Hash()
	txhash := signedtx.Hash()
	isSuccess := this.waitTransactionConfirm(fmt.Sprintf("header: %d", header.Height), txhash)
	if isSuccess {
		log.Infof("successful to relay poly header to ethereum: (header_hash: %s, height: %d, eth_txhash: %s, nonce: %d, eth_explorer: %s)",
			hash.ToHexString(), header.Height, txhash.String(), nonce, tools.GetExplorerUrl(this.keyStore.GetChainId())+txhash.String())
	} else {
		log.Errorf("failed to relay poly header to ethereum: (header_hash: %s, height: %d, eth_txhash: %s, nonce: %d, eth_explorer: %s)",
			hash.ToHexString(), header.Height, txhash.String(), nonce, tools.GetExplorerUrl(this.keyStore.GetChainId())+txhash.String())
	}
	return true
}

func (this *EthSender) Balance() (*big.Int, error) {
	balance, err := this.ethClient.BalanceAt(context.Background(), this.acc.Address, nil)
	if err != nil {
		return nil, err
	}
	return balance, nil
}

// TODO: check the status of tx
func (this *EthSender) waitTransactionConfirm(polyTxHash string, hash ethcommon.Hash) bool {
	for {
		time.Sleep(time.Second * 1)
		_, ispending, err := this.ethClient.TransactionByHash(context.Background(), hash)
		if err != nil {
			continue
		}
		log.Debugf("( eth_transaction %s, poly_tx %s ) is pending: %v", hash.String(), polyTxHash, ispending)
		if ispending == true {
			continue
		} else {
			receipt, err := this.ethClient.TransactionReceipt(context.Background(), hash)
			if err != nil {
				continue
			}
			return receipt.Status == types.ReceiptStatusSuccessful
		}
	}
}

type EthTxInfo struct {
	txData       []byte
	gasLimit     uint64
	gasPrice     *big.Int
	contractAddr ethcommon.Address
	polyTxHash   string
}
