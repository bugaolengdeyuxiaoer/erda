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

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/modules/openapi/api/apis"
	"github.com/erda-project/erda/modules/orchestrator/utils"
	"github.com/erda-project/erda/pkg/discover"
	"github.com/erda-project/erda/pkg/http/httpclient"
)

var MSP_METRIC_PROJECT_OVERVIEW = apis.ApiSpec{
	Path:        "/api/msp/metrics/tenant/project/overview",
	BackendPath: "/api/msp/metrics/tenant/project/overview",
	Host:        "msp.marathon.l4lb.thisdcos.directory:8080",
	Scheme:      "http",
	Method:      "POST",
	CheckLogin:  true,
	CheckToken:  true,
	Custom:      attachMetricProjectParams,
	IsOpenAPI:   true,
	Doc:         "GET MSP project overview for dashboard",
}

func attachMetricProjectParams(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("User-ID")
	orgID := r.Header.Get("Org-ID")
	lang := r.Header.Get("lang")

	// get permissions
	client := httpclient.New()
	var perms apistructs.ScopeRoleList
	cr := client.Get(discover.CoreServices()).
		Path("/api/permissions").
		Header("User-ID", userID).
		Header("Org-ID", orgID)
	if err := utils.DoJson(cr, &perms); err != nil {
		ErrFromError(w, err)
	}

	params := r.URL.Query()
	key := params.Get("project_key")
	params.Del("project_key")
	if key == "gateway" {
		paramsForProject := url.Values{}
		paramsForProject.Del("projectId")
		for _, p := range perms.List {
			if p.Scope.Type == apistructs.ProjectScope && p.Access {
				paramsForProject.Add("projectId", p.Scope.ID)
			}
		}
		var data []string
		cr = client.Get(discover.MSP()).
			Header("lang", lang).
			Header("User-ID", userID).
			Header("Org-ID", orgID).
			Path("/api/msp/tenant/projects/tenants/ids").
			Params(paramsForProject)
		if err := utils.DoJson(cr, &data); err != nil {
			ErrFromError(w, err)
			return
		}

		for _, id := range data {
			params.Add("in__metric_scope_id", id)
		}
	} else {
		fk := fmt.Sprintf("in_%s", key)
		params.Del(fk)
		for _, p := range perms.List {
			if p.Scope.Type == apistructs.ProjectScope && p.Access {
				params.Add(fk, p.Scope.ID)
			}
		}
	}

	var data json.RawMessage
	cr = client.Post(discover.MSP()).
		Header("lang", lang).
		Header("User-ID", userID).
		Header("Org-ID", orgID).
		Path(r.URL.Path).
		Params(params).
		RawBody(r.Body)

	if err := utils.DoJson(cr, &data); err != nil {
		ErrFromError(w, err)
		return
	}

	Success(w, data)
}
