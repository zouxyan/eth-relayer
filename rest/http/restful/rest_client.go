/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

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
package restful

import (
	"bytes"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

var (
	FlamCli *RestClient
)

type Response struct {
	Message string `json:"message"`
	Token   string `json:"token"`
}

type RestClient struct {
	Addr       string
	restClient *http.Client
	Jwt        string
}

func NewRestClient(addr string) *RestClient {
	return &RestClient{
		restClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost:   5,
				DisableKeepAlives:     false,
				IdleConnTimeout:       time.Second * 300,
				ResponseHeaderTimeout: time.Second * 300,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: time.Second * 300,
		},
		Addr: addr,
	}
}

func (self *RestClient) SetAddr(addr string) *RestClient {
	self.Addr = addr
	return self
}

func (self *RestClient) SetRestClient(restClient *http.Client) *RestClient {
	self.restClient = restClient
	return self
}

func (self *RestClient) SendRequest(method, addr string, data []byte) ([]byte, error) {
	var (
		resp *http.Response
		err  error
	)
	req, err := http.NewRequest(method, addr, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+self.Jwt)
	resp, err = self.restClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request error:%s", err)
	}
	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("response shows unauthorized")
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rest response body error:%s", err)
	}
	return body, nil
}

func (self *RestClient) GetJWTToken(secret string) (string, error) {
	query, err := json.Marshal(map[string]interface{}{
		"code":     secret,
		"clientId": "eth_relayer",
	})
	if err != nil {
		return "", fmt.Errorf("Failed to parse query parameter: %v", err)
	}

	data, err := self.SendRequest("POST", "http://"+self.Addr+"/auth", query)
	if err != nil {
		return "", fmt.Errorf("Failed to send request: %v", err)
	}
	resp := &Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return "", fmt.Errorf("Failed to unmarshal resp to json: %v", err)
	}
	if resp.Message != "" {
		return "", fmt.Errorf("wrong response: %s", resp.Message)
	}
	self.Jwt = resp.Token

	return self.Jwt, nil
}

func (self *RestClient) SendEthInfo(toChainId, amount uint64, toContract, method, toAddress, toAssetHash, originTx string, args []string) error {
	query, err := json.Marshal(map[string]interface{}{
		"rawData": map[string]interface{}{
			"chainId":      toChainId,
			"contractHash": toContract, // TODO with `0x` ?
			"method":       method,
			"args":         args,
		},
		"toAddress":      toAddress,
		"toAssetHash":    toAssetHash,
		"amountReceived": amount,
		"originalTxHash": originTx,
	})
	if err != nil {
		return fmt.Errorf("Failed to parse query parameter: %v", err)
	}

	data, err := self.SendRequest("PUT", "http://"+self.Addr+"/frompoly/"+originTx, query)
	if err != nil {
		return fmt.Errorf("Failed to send request: %v", err)
	}
	resp := &Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal resp to json: %v", err)
	}
	if resp.Message != "" {
		return fmt.Errorf("server return error msg: %s, raw_query: %s", resp.Message, hex.EncodeToString(query))
	}
	return nil
}

func (self *RestClient) SendTxPair(fromChainId, toChainId uint32, fromWallet, toWallet, polyTxHash, ethTxHash, fromAssetHash string, amount uint64, relayerStatus int) error {
	query, err := json.Marshal(map[string]interface{}{
		"fromAddress":         fromWallet,
		"toAddress":           toWallet,
		"fromChainId":         fromChainId,
		"toChainId":           toChainId,
		"fromAssetHash":       fromAssetHash,
		"amountSent":          amount,
		"originalTxHash":      ethTxHash,
		"toPolyRelayerStatus": relayerStatus,
		"toPolyRelayerTxHash": polyTxHash,
	})
	if err != nil {
		return fmt.Errorf("Failed to parse query parameter: %v", err)
	}

	data, err := self.SendRequest("PUT", "http://"+self.Addr+"/topoly", query)
	if err != nil {
		return fmt.Errorf("Failed to send request: %v", err)
	}
	resp := &Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal resp to json: %v", err)
	}

	if resp.Message != "" {
		return fmt.Errorf("server return error: msg: %s, raw_query: %s", resp.Message, hex.EncodeToString(query))
	}
	return nil
}

func (self *RestClient) UpdateTxPair(fromChainId, toChainId uint32, fromWallet, toWallet, polyTxHash, ethTxHash, fromAssetHash string, amount uint64, polyStatus int) error {
	query, err := json.Marshal(map[string]interface{}{
		"fromAddress":         fromWallet,
		"toAddress":           toWallet,
		"fromChainId":         fromChainId,
		"toChainId":           toChainId,
		"fromAssetHash":       fromAssetHash,
		"amountSent":          amount,
		"originalTxHash":      ethTxHash,
		"toPolyRelayerStatus": 1,
		"toPolyRelayerTxHash": polyTxHash,
		"polyStatus":          polyStatus,
	})
	if err != nil {
		return fmt.Errorf("Failed to parse query parameter: %v", err)
	}

	data, err := self.SendRequest("PATCH", "http://"+self.Addr+"/topoly/"+ethTxHash, query)
	if err != nil {
		return fmt.Errorf("Failed to send request: %v", err)
	}
	resp := &Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal resp to json: %v", err)
	}

	if resp.Message != "" {
		return fmt.Errorf("server return error msg: %s, raw_query: %s", resp.Message, hex.EncodeToString(query))
	}
	return nil
}

func (self *RestClient) SendConfirmedTxArr(arr []string) error {
	query, err := json.Marshal(map[string]interface{}{
		"originalTxHashList": arr,
		"updateData": map[string]interface{}{
			"destinationTxStatus": 1,
		},
	})
	if err != nil {
		return fmt.Errorf("Failed to parse query parameter: %v", err)
	}

	data, err := self.SendRequest("PATCH", "http://"+self.Addr+"/frompoly", query)
	if err != nil {
		return fmt.Errorf("Failed to send request: %v", err)
	}
	resp := &Response{}
	err = json.Unmarshal(data, resp)
	if err != nil {
		return fmt.Errorf("Failed to unmarshal resp to json: %v", err)
	}

	if resp.Message != "" {
		return fmt.Errorf("server return error msg: %s, raw_query: %s", resp.Message, hex.EncodeToString(query))
	}
	return nil
}
