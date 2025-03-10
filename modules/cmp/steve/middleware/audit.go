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

package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/erda-project/erda/apistructs"
	"github.com/erda-project/erda/bundle"
	"github.com/erda-project/erda/pkg/http/httputil"
	"github.com/erda-project/erda/pkg/strutil"
)

const (
	// audit template name
	auditCordonNode     = "cordonNode"
	auditUncordonNode   = "uncordonNode"
	auditLabelNode      = "labelNode"
	auditUnLabelNode    = "unLabelNode"
	auditUpdateResource = "updateK8SResource"
	auditCreateResource = "createK8SResource"
	auditDeleteResource = "deleteK8SResource"
	auditKubectlShell   = "kubectlShell" // TODO

	// audit template params
	auditClusterName  = "clusterName"
	auditNamespace    = "namespace"
	auditResourceType = "resourceType"
	auditResourceName = "name"
	auditTargetLabel  = "targetLabel"
	auditCommands     = "commands" // TODO
)

type Auditor struct {
	bdl *bundle.Bundle
}

// NewAuditor return a steve Auditor with bundle.
// bdl needs withCoreServices to create audit events.
func NewAuditor(bdl *bundle.Bundle) *Auditor {
	return &Auditor{bdl: bdl}
}

// AuditMiddleWare audit for steve server by bundle.
func (a *Auditor) AuditMiddleWare(next http.Handler) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		var body []byte
		if req.Body != nil {
			body, _ = ioutil.ReadAll(req.Body)
		}
		req.Body = ioutil.NopCloser(bytes.NewBuffer(body))

		writer := &wrapWriter{
			ResponseWriter: resp,
			statusCode:     http.StatusOK,
		}
		next.ServeHTTP(writer, req)

		if body == nil {
			return
		}
		if writer.statusCode/100 != 2 {
			return
		}

		vars := parseVars(req)
		clusterName := vars["clusterName"]
		typ := vars["type"]
		if typ == "" {
			return
		}
		namespace := vars["namespace"]
		name := vars["name"]
		isInternal := req.Header.Get(httputil.InternalHeader) != ""
		userID := req.Header.Get(httputil.UserHeader)
		orgID := req.Header.Get(httputil.OrgHeader)
		scopeID, _ := strconv.ParseUint(orgID, 10, 64)
		now := strconv.FormatInt(time.Now().Unix(), 10)

		//logrus.Infof("Get request. User-ID: %s, Org-ID: %s", userID, orgID)

		auditReq := apistructs.AuditCreateRequest{
			Audit: apistructs.Audit{
				UserID:    userID,
				ScopeType: apistructs.OrgScope,
				ScopeID:   scopeID,
				OrgID:     scopeID,
				Result:    "success",
				StartTime: now,
				EndTime:   now,
				ClientIP:  getRealIP(req),
				UserAgent: req.UserAgent(),
			},
		}

		ctx := make(map[string]interface{})
		switch req.Method {
		case http.MethodPatch:
			if isInternal && strutil.Equal(typ, "nodes", true) {
				var rb reqBody
				if err := json.Unmarshal(body, &rb); err != nil {
					logrus.Errorf("failed to unmarshal in steve audit")
					return
				}

				// audit for label/unlabel node
				if rb.Metadata != nil && rb.Metadata["labels"] != nil {
					labels, _ := rb.Metadata["labels"].(map[string]interface{})
					var (
						k string
						v interface{}
					)
					for k, v = range labels {
					} // there can only be one piece of k/v
					if v == nil {
						auditReq.Audit.TemplateName = auditUnLabelNode
						ctx[auditTargetLabel] = k
					} else {
						auditReq.Audit.TemplateName = auditLabelNode
						ctx[auditTargetLabel] = fmt.Sprintf("%s=%s", k, v.(string))
					}
					break
				}

				// audit for cordon/uncordon node
				if rb.Spec != nil && rb.Spec["unschedulable"] != nil {
					v, _ := rb.Spec["unschedulable"].(bool)
					if v {
						auditReq.Audit.TemplateName = auditCordonNode
					} else {
						auditReq.Audit.TemplateName = auditUncordonNode
					}
				}
				break
			}
			fallthrough
		case http.MethodPut:
			auditReq.Audit.TemplateName = auditUpdateResource
		case http.MethodPost:
			auditReq.Audit.TemplateName = auditCreateResource
			var rb reqBody
			if err := json.Unmarshal(body, &rb); err != nil {
				logrus.Errorf("failed to unmarshal in steve audit")
				return
			}
			data := rb.Metadata["name"]
			if n, ok := data.(string); ok && n != "" {
				name = n
			}
			data = rb.Metadata["namespace"]
			if ns, ok := data.(string); ok && namespace != "" {
				namespace = ns
			}
		case http.MethodDelete:
			auditReq.Audit.TemplateName = auditDeleteResource
		default:
			return
		}

		ctx[auditClusterName] = clusterName
		ctx[auditResourceName] = name
		ctx[auditNamespace] = namespace
		ctx[auditResourceType] = typ
		auditReq.Context = ctx

		if err := a.bdl.CreateAuditEvent(&auditReq); err != nil {
			logrus.Errorf("faild to audit in steve audit, %v", err)
		}
	})
}

type reqBody struct {
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Spec     map[string]interface{} `json:"spec,omitempty"`
}

type wrapWriter struct {
	http.ResponseWriter
	statusCode int
	buf        bytes.Buffer
}

func (w *wrapWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	w.statusCode = statusCode
}

func (w *wrapWriter) Write(body []byte) (int, error) {
	w.buf.Write(body)
	return w.ResponseWriter.Write(body)
}

func getRealIP(request *http.Request) string {
	ra := request.RemoteAddr
	if ip := request.Header.Get("X-Forwarded-For"); ip != "" {
		ra = strings.Split(ip, ", ")[0]
	} else if ip := request.Header.Get("X-Real-IP"); ip != "" {
		ra = ip
	} else {
		ra, _, _ = net.SplitHostPort(ra)
	}
	return ra
}

func parseVars(req *http.Request) map[string]string {
	var match mux.RouteMatch
	m := mux.NewRouter().PathPrefix("/api/k8s/clusters/{clusterName}")
	s := m.Subrouter()
	s.Path("/v1/{type}")
	s.Path("/v1/{type}/{name}")
	s.Path("/v1/{type}/{namespace}/{name}")
	s.Path("/v1/{type}/{namespace}/{name}/{link}")
	s.Path("/api/v1/namespaces/{namespace}/{type}/{name}/{link}")

	vars := make(map[string]string)
	if s.Match(req, &match) {
		vars = match.Vars
	}
	return vars
}
