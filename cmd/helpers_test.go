package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPartitionID = "deb04125-c1a2-371e-b263-6a76f0a90e94"
	testRegion      = "stg-west2"
)

func TestERSConfig(t *testing.T) {
	t.Parallel()

	t.Run("local defaults", func(t *testing.T) {
		t.Parallel()

		endpoint, headers, err := ersConfig("local", "", "", "")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:9300", endpoint)
		assert.Equal(t, map[string]string{
			"X-Slauth-Mechanism": "asap",
			"X-Slauth-Issuer":    "loom/loom",
			"Partition-Id":       "tdp-ers-local",
		}, headers)
	})

	t.Run("local honours overrides", func(t *testing.T) {
		t.Parallel()

		endpoint, headers, err := ersConfig("local", "http://localhost:9999", "my-partition", "")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:9999", endpoint)
		assert.Equal(t, "my-partition", headers["Partition-Id"])
	})

	t.Run("staging", func(t *testing.T) {
		t.Parallel()

		endpoint, headers, err := ersConfig("staging", "", testPartitionID, testRegion)
		require.NoError(t, err)
		assert.Equal(t, "http://istio-egress-internal.istio-egress-internal.svc.cluster.local", endpoint)
		assert.Equal(t, map[string]string{
			"X-Atlassian-Host":                 "ers-data.sgw.staging.atl-paas.net",
			"X-Slauth-Egress":                  "true",
			"X-Slauth-Audience":                "ers-data",
			"atl-sp-archetype":                 "ers-data-archetype",
			"Partition-Id":                     testPartitionID,
			"atl-paas-sp-consumer-environment": testRegion,
		}, headers)
	})

	t.Run("production uses the prod host", func(t *testing.T) {
		t.Parallel()

		_, headers, err := ersConfig("production", "", "prod-partition", "prod-west2")
		require.NoError(t, err)
		assert.Equal(t, "ers-data.sgw.prod.atl-paas.net", headers["X-Atlassian-Host"])
	})

	t.Run("staging requires partition id and region", func(t *testing.T) {
		t.Parallel()

		_, _, err := ersConfig("staging", "", "", testRegion)
		require.ErrorContains(t, err, "--ers-partition-id and --ers-region are required")

		_, _, err = ersConfig("staging", "", testPartitionID, "")
		require.ErrorContains(t, err, "--ers-partition-id and --ers-region are required")
	})

	t.Run("unknown env", func(t *testing.T) {
		t.Parallel()

		_, _, err := ersConfig("prod", "", testPartitionID, testRegion)
		assert.ErrorContains(t, err, `unknown --ers-env "prod"`)
	})
}
