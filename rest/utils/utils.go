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
package utils

import (
	"encoding/json"
	"fmt"
	"github.com/polynetwork/eth_relayer/rest/http/common"
	"math/big"
)

func ParseParams(req interface{}, params map[string]interface{}) error {
	jsonData, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("ParseParams: marshal params failed, err: %s", err)
	}
	err = json.Unmarshal(jsonData, req)
	if err != nil {
		return fmt.Errorf("ParseParams: unmarshal req failed, err: %s", err)
	}
	return nil
}

func RefactorResp(resp *common.Response, errCode uint32) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		return m, fmt.Errorf("RefactorResp: marhsal resp failed, err: %s", err)
	}
	err = json.Unmarshal(jsonResp, &m)
	if err != nil {
		return m, fmt.Errorf("RefactorResp: unmarhsal resp failed, err: %s", err)
	}
	m["error"] = errCode
	return m, nil
}

func bytesReverse(u []byte) []byte {
	for i, j := 0, len(u)-1; i < j; i, j = i+1, j-1 {
		u[i], u[j] = u[j], u[i]
	}
	return u
}

func BigIntFromNeoBytes(ba []byte) *big.Int {
	res := big.NewInt(0)
	l := len(ba)
	if l == 0 {
		return res
	}

	bytes := make([]byte, 0, l)
	bytes = append(bytes, ba...)
	bytesReverse(bytes)

	if bytes[0]>>7 == 1 {
		for i, b := range bytes {
			bytes[i] = ^b
		}

		temp := big.NewInt(0)
		temp.SetBytes(bytes)
		temp.Add(temp, big.NewInt(1))
		bytes = temp.Bytes()
		res.SetBytes(bytes)
		return res.Neg(res)
	}

	res.SetBytes(bytes)
	return res
}

//// only for cross chain
//func EstimateSerializedTxSize(inputCount int, txOuts []*wire.TxOut) int {
//	multi5of7InputSize := 32 + 4 + 1 + 4 + btc-vendor-tools.RedeemP2SH5of7MultisigSigScriptSize
//
//	outsSize := 0
//	for _, txOut := range txOuts {
//		outsSize += txOut.SerializeSize()
//	}
//
//	return 10 + wire.VarIntSerializeSize(uint64(inputCount)) + wire.VarIntSerializeSize(uint64(len(txOuts)+1)) +
//		inputCount*multi5of7InputSize + btc-vendor-tools.MaxP2SHScriptSize + outsSize
//}
