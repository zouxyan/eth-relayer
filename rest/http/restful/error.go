// Package error privides error code for rest
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

const (
	SUCCESS uint32 = 0
	FAILED  uint32 = 1

	INVALID_METHOD     uint32 = 42001
	INVALID_PARAMS     uint32 = 42002
	ILLEGAL_DATAFORMAT uint32 = 42003
	INTERNAL_ERROR     uint32 = 42004
)

var ErrMap = map[uint32]string{
	SUCCESS:            "SUCCESS",
	FAILED:             "FAILED",
	INVALID_METHOD:     "INVALID METHOD",
	INVALID_PARAMS:     "INVALID PARAMS",
	ILLEGAL_DATAFORMAT: "ILLEGAL DATAFORMAT",
	INTERNAL_ERROR:     "INTERNAL_ERROR",
}
