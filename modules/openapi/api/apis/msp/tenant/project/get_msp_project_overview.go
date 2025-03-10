// Copyright (c) 2021 Terminus, Inc.
//
// This program is free software: you can use, redistribute, and/or modify
// it under the terms of the GNU Affero General Public License, version 3
// or later ("AGPL"), as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package project

import "github.com/erda-project/erda/modules/openapi/api/apis"

var MSP_PROJECT_OVERVIEW = apis.ApiSpec{
	Path:        "/api/msp/tenant/project/overview",
	BackendPath: "/api/msp/tenant/project/overview",
	Host:        "msp.marathon.l4lb.thisdcos.directory:8080",
	Scheme:      "http",
	Method:      "GET",
	CheckLogin:  true,
	CheckToken:  true,
	Custom:      attachProjectParams,
	IsOpenAPI:   true,
	Doc:         "GET MSP project overview for dashboard",
}
