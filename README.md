# metrics-proxy-rock


[![Open a PR to OCI Factory](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-release-oci-factory.yaml/badge.svg)](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-release-oci-factory.yaml)
[![Publish to GHCR:dev](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-release-dev.yaml/badge.svg)](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-release-dev.yaml)
[![Update rock](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-update.yaml/badge.svg)](https://github.com/canonical/metrics-proxy-rock/actions/workflows/rock-update.yaml)

[Rocks](https://canonical-rockcraft.readthedocs-hosted.com/en/latest/) for the **Metrics Proxy**.  
This repository holds all the necessary files to build a rock for the **metrics-proxy**, which aggregates metrics from Kubernetes pods with Prometheus service discovery annotations.


The rocks on this repository are built with [OCI Factory](https://github.com/canonical/oci-factory/), which also takes care of periodically rebuilding the images.

Automation takes care of:
* validating PRs, by simply trying to build the rock;
* pulling upstream releases, creating a PR with the necessary files to be manually reviewed;
* releasing to GHCR at [ghcr.io/canonical/metrics-proxy:dev](https://ghcr.io/canonical/metrics-proxy:dev), when merging to main, for development purposes.