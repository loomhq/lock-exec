// Package ers implements a minimal, hand-rolled HTTP client for Atlassian's TDP Entity
// Relationship Store (ERS), used as an alternative backend for lock-exec's distributed locking.
//
// The wire protocol implemented here was initially reverse-engineered from Atlassian's
// official TypeScript client (bitbucket.org/atlassian/tdp-clients, package ers-nodejs-sdk,
// src/client/base-client.ts) since no official Go client exists yet, then confirmed and
// corrected directly against a live `atlas tdp ers local up` instance's own /api/oas
// OpenAPI spec and real request/response traffic (the TS SDK's public interfaces describe a
// simpler shape than the actual wire protocol -- notably, custom schema properties like our
// "expire" field must be nested under a "properties" object on every request/response, not
// flat). See:
//   - POST   /nodes                              create a node (data plane, port 9300 locally)
//   - GET    /nodes/{id}?type={schema}            read a node (404 if absent)
//   - PATCH  /nodes/{id}?type={schema}            update a node (version-checked, OCC)
//   - DELETE /nodes/{id}?type={schema}            delete a node
//
// Every request also requires a "Partition-Id" header identifying an existing ERS partition
// (see NewLocalDevHeaders/NewProductionHeaders) -- confirmed: omitting it returns a 422
// "One of Partition-Id OR PAAS Context OR PAAS Baggage must be provided".
//
// A 409 response on create/update means an optimistic-concurrency conflict: either a node
// with that id already exists (idConflictPolicy=FAIL on create) or the supplied version is
// stale (on update).
//
// Note: `atlas tdp ers local up` starts two separate services -- an ERS *control* plane
// (schema/partition management, port 9301 locally) and an ERS *data* plane (node CRUD, the
// API this package implements, port 9300 locally). Pointing this client at the control
// plane fails with a generic Spring "No static resource nodes" 404, not a helpful error.
package ers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	// idConflictPolicyFail tells ERS to reject node creation if a node with the same id
	// already exists, rather than silently overwriting it.
	idConflictPolicyFail = "FAIL"

	// statusConflict is the HTTP status ERS returns for an optimistic-concurrency failure,
	// on both node creation (id already exists) and node update (stale version).
	statusConflict = http.StatusConflict

	// SchemaType is lock-exec's ERS schema type, defined in loom-tdp-artifacts
	// (tdp-ers/lock-exec-v1.json).
	SchemaType = "ati:loom:infra:lock-exec"

	// SchemaVersion is the ERS schema version lock-exec writes against.
	SchemaVersion = 1

	// queryParamType is the query parameter naming the node's schema type on reads,
	// updates and deletes.
	queryParamType = "type"
)

// ErrConflict is returned by Client methods when ERS reports a 409 optimistic-concurrency
// conflict: either the node already exists (on create) or the version is stale (on update).
var ErrConflict = fmt.Errorf("ers: optimistic concurrency conflict")

// ErrNotFound is returned by Get when no node exists for the given id.
var ErrNotFound = fmt.Errorf("ers: node not found")

// Client is a minimal HTTP client for a single ERS schema/type. It is not a general-purpose
// ERS client -- it only implements what lock-exec needs (single-partition, no sort key,
// consumer-provided ids).
type Client struct {
	httpClient    *http.Client
	baseURL       string
	schemaType    string
	schemaVersion int
	headers       map[string]string
}

// NewClient creates a new ERS client scoped to a single schema type/version.
//
//   - baseURL: e.g. "http://localhost:9300" for local ERS-in-a-box, or the
//     istio-egress-internal gateway for staging/production,
//     "http://istio-egress-internal.istio-egress-internal.svc.cluster.local"
//     (see NewProductionHeaders).
//   - schemaType: the ERS schema type, e.g. "ati:loom:infra:lock-exec".
//   - schemaVersion: the schema version to write against.
//   - headers: static headers added to every request (auth headers -- see
//     NewLocalDevHeaders / NewProductionHeaders).
func NewClient(httpClient *http.Client, baseURL, schemaType string, schemaVersion int, headers map[string]string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		httpClient:    httpClient,
		baseURL:       baseURL,
		schemaType:    schemaType,
		schemaVersion: schemaVersion,
		headers:       headers,
	}
}

// NewLocalDevHeaders returns the header set that works against a local
// `atlas tdp ers local up` instance, per the loom repo's own
// projects/repo-tools/devenv/tdp/ers/start-local-tdp-ers.sh example.
//
// partitionID is required on every ERS data-plane request (confirmed against a live
// local instance: ERS returns 422 "One of Partition-Id OR PAAS Context OR PAAS Baggage
// must be provided" without it) -- pass the partition id to write into, e.g.
// "tdp-ers-local" for the seeded local dev partition (see local-env-seed/partitions/).
func NewLocalDevHeaders(partitionID string) map[string]string {
	return map[string]string{
		"X-Slauth-Mechanism": "asap",
		"X-Slauth-Issuer":    "loom/loom",
		"Partition-Id":       partitionID,
	}
}

// NewProductionHeaders returns the header set required to reach real ERS in staging or
// production through the istio-egress-internal gateway, using non-PMR (partition-addressed)
// access. No ASAP token is signed client-side: these headers tell the gateway to mint one
// (as the shared loom/loom identity) and forward the request. The client connects to the
// gateway itself (baseURL -- see NewClient), not to host.
//
// Verified against real staging ERS from staging-olive (see load-exec-project-scoping.md
// Section 19 in infra-terraform):
//
//   - host: the ERS service-gateway host, "ers-data.sgw.staging.atl-paas.net" or
//     "ers-data.sgw.prod.atl-paas.net". It must be in the mesh's egressHosts allowlist.
//     Don't use atlassian-proxy: that's the tenant-routed (PMR) entry point, which requires
//     a workspace sharding context that locks don't have.
//   - partitionID: the non-tenanted partition that holds lock records.
//   - region: the partition's ERS region, e.g. "stg-west2" (the "environment" column of
//     `atlas tdp get-partitions`). The service gateway needs it to route to the right shard.
func NewProductionHeaders(host, partitionID, region string) map[string]string {
	return map[string]string{
		"X-Atlassian-Host":                 host,
		"X-Slauth-Egress":                  "true",
		"X-Slauth-Audience":                "ers-data",
		"atl-sp-archetype":                 "ers-data-archetype",
		"Partition-Id":                     partitionID,
		"atl-paas-sp-consumer-environment": region,
	}
}

// nodeProperties is the JSON shape of lock-exec's single custom schema property, as it
// appears nested under "properties" on create requests and on every response. Confirmed
// against a live ERS instance: a flat top-level "expire" field (which is what the
// TypeScript SDK's public request/response *interfaces* suggested) is rejected with a 422
// "Mandatory Property: expire Is Missing!" -- custom schema properties must be nested.
type nodeProperties struct {
	Expire int64 `json:"expire"`
}

// createNodeRequest is the POST /nodes request body.
type createNodeRequest struct {
	ID               string         `json:"id"`
	Type             string         `json:"type"`
	SchemaVersion    int            `json:"schemaVersion"`
	IDConflictPolicy string         `json:"idConflictPolicy,omitempty"`
	Properties       nodeProperties `json:"properties"`
}

// propertyUpdate is ERS's "action"-based property mutation shape, required on every
// property inside an update request's "properties" object (confirmed against a live
// instance; a bare value there is rejected).
type propertyUpdate struct {
	Action string `json:"action"`
	Value  int64  `json:"value"`
}

// updateNodeProperties is the "properties" object for PATCH /nodes/{id} requests.
type updateNodeProperties struct {
	Expire propertyUpdate `json:"expire"`
}

// updateNodeRequest is the PATCH /nodes/{id} request body. Type is required in the body in
// addition to being a query parameter -- confirmed: omitting it from the body returns a 422
// "type: must not be blank" even though it's also passed as ?type=. Version is ERS's
// "simple" optimistic-concurrency shorthand (a plain integer-equality check) -- an
// alternative to the more general conditions/FilterExpression mechanism the API also
// supports, which lock-exec doesn't need.
type updateNodeRequest struct {
	Type       string               `json:"type"`
	Version    int64                `json:"version"`
	Properties updateNodeProperties `json:"properties"`
}

// nodeResponse is the JSON shape ERS returns from GET/POST/PATCH /nodes(/{id}).
type nodeResponse struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	SchemaVersion int            `json:"schemaVersion"`
	Version       int64          `json:"version"`
	Properties    nodeProperties `json:"properties"`
}

// node is a flattened, convenience view of a nodeResponse -- just the two fields
// lock-exec's storageI translation (storage.go) actually needs.
type node struct {
	Version int64
	Expire  int64
}

// Create creates a new node with the given id and expire value. Returns ErrConflict
// (wrapping the 409) if a node with that id already exists.
func (c *Client) Create(ctx context.Context, id string, expire int64) error {
	body := createNodeRequest{
		ID:               id,
		Type:             c.schemaType,
		SchemaVersion:    c.schemaVersion,
		IDConflictPolicy: idConflictPolicyFail,
		Properties:       nodeProperties{Expire: expire},
	}

	_, err := c.do(ctx, http.MethodPost, "/nodes", nil, body)
	return err
}

// Get reads a node by id. Returns ErrNotFound if no node exists for that id.
func (c *Client) Get(ctx context.Context, id string) (*node, error) {
	path := "/nodes/" + url.PathEscape(id)
	query := url.Values{queryParamType: {c.schemaType}}

	resp, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, ErrNotFound
	}

	var n nodeResponse
	if err := json.Unmarshal(resp, &n); err != nil {
		return nil, fmt.Errorf("ers: failed to decode node: %w", err)
	}

	return &node{Version: n.Version, Expire: n.Properties.Expire}, nil
}

// Update performs an optimistic-concurrency-checked update of an existing node's expire
// value. version must be the version most recently read via Get. Returns ErrConflict
// (wrapping the 409) if version is stale -- i.e. someone else updated the node first.
func (c *Client) Update(ctx context.Context, id string, version, expire int64) error {
	path := "/nodes/" + url.PathEscape(id)
	query := url.Values{queryParamType: {c.schemaType}}

	body := updateNodeRequest{
		Type:    c.schemaType,
		Version: version,
		Properties: updateNodeProperties{
			Expire: propertyUpdate{Action: "set", Value: expire},
		},
	}

	_, err := c.do(ctx, http.MethodPatch, path, query, body)
	return err
}

// Delete deletes a node by id. Unlike Create/Update this is unconditional -- it does not
// check ownership or version, matching lock-exec's existing DynamoDB Unlock() semantics
// (see lock/lock.go Unlock, and the doc's Section 14 on the lack of ownership enforcement).
// Deleting an already-absent node is not an error.
func (c *Client) Delete(ctx context.Context, id string) error {
	path := "/nodes/" + url.PathEscape(id)
	query := url.Values{queryParamType: {c.schemaType}}

	_, err := c.do(ctx, http.MethodDelete, path, query, nil)
	if err != nil && !isNotFound(err) {
		return err
	}

	return nil
}

// do performs an HTTP request against the ERS API, injecting configured headers, encoding
// the request body as JSON if present, and mapping non-2xx responses to sentinel errors.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("ers: failed to encode request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("ers: failed to build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ers: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ers: failed to read response body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == statusConflict:
		return nil, ErrConflict
	case resp.StatusCode >= http.StatusBadRequest:
		return nil, fmt.Errorf("ers: unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
