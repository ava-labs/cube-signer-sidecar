# Cube Signer Sidecar

This repository contains a proxy that can be run alongside [AvalancheGo](https://github.com/ava-labs/avalanchego) nodes to integrate them with [CubeSigner](https://cubist.dev/products/cubesigner-self-custody) for secure BLS key management powered by [Cubist](https://cubist.dev/). AvalancheGo nodes currently use BLS keys for peer handshakes and to sign [ICM messages](https://build.avax.network/docs/cross-chain/avalanche-warp-messaging/overview). By integrating with CubeSigner, AvalancheGo nodes do not need to store their BLS keys in memory or on disk. Instead, the keys are generated and kept within a remote hardware security module (HSM), and the AvalancheGo node is configured to request signatures from that HSM as needed via this `cube-signer-sidecar`.

This service runs a [gRPC](https://grpc.io/) server, implementing the [`signer.proto` service definition](https://github.com/ava-labs/avalanchego/blob/master/proto/signer/signer.proto). When the `gRPC` endpoints are hit, they make subsequent requests to the [CubeSigner API](https://signer-docs.cubist.dev/api) to get the requested signature.

A Cubist account is required to be able to properly use the `cube-signer-sidecar`.

## Building

The `api/` directory contains generated code from the [CubeSigner OpenAPI specification](https://raw.githubusercontent.com/cubist-labs/CubeSigner-TypeScript-SDK/main/packages/sdk/spec/openapi.json). The [`spec/get-schemas.go`] script is used to filter the API-spec for the three relevant endpoints, as well as all the schemas that those endpoints
use. The filtered Open-API specification is output to `spec/filtered-openapi.json`.

If there are changes in `spec/filtered-openapi.json`, the `go generate ./signerserver` _must_ be run to re-generate the client code in the `api/` directory.

## Testing

The `cube-signer-sidecar` depends on the AvalancheGo changes implemented in [this PR](https://github.com/ava-labs/avalanchego/pull/3965). In order to test it, set the `--staking-rpc-signer-endpoint=127.0.0.1:50051` configuration flag, and ensure that the `cube-signer-sidecar` application is running before starting the `avalanchego` node.

## Running

### Key Creation

The [`CubeSigner`](https://github.com/cubist-partners/CubeSigner/) application is needed to set up the `cube-signer-sidecar` to be run locally. Once installed, set up the `CubeSigner` and log in following the [Getting Started instructions](https://signer-docs.cubist.dev/getting-started). The following commands can then be used to set up a role, key, and signing policy. 

```shell
# Create a role.
cs role create --role-name bls_signer

# Create a key.
cs keys create --key-type=bls-ava-icm

# Set the signing policy for the key.
# The `cs keys list` command can be used to find the <key_id>.
cs key set-policy --key-id <key_id> --policy '"AllowRawBlobSigning"'

# Add the key to the role.
# Either the full <role_id> or the <role_name> (i.e. "bls_signer") can be used.
cs role add-key --role-id <role_id> --key-id <key_id>

# Finally, create a token file associated with the role.
cs token create --role-id <role_id> > <path_to_token_file>.json
```

### Configuration

Below is a list of configuration options that can be set via a JSON config file passed in via `--config-file` flag or set through environment variables or flags. To get the environment variable corresponding to the key uppercase the key and change the delimiter from "-" to "_". The following precedence order is used, with each item taking precedence over items below it:

1. Flags
2. Environment variables
3. Config file

- `"token-file-path": string` (required)

  This is the path to the token file, created in the last step above.

  The `refresh-token` (part of the JSON output of `cs token create`) has a short TTL by default, and the `cube-signer-sidecar` must be started before it expires. Once started, the `<path_to_token>.json` file will be continuously refreshed as needed. To change any of the default token parameters, see `cs token create --help`.

- `"signer-endpoint": string` (required)

  The CubeSigner API endpoint.

- `"key-id": string` (required)

  The `cube-signer-sidecar` can only use one key at a time, as an `avalanchego` validator is only meant to have a single BLS signing key. Specifying the `KEY_ID` is how the CubeSigner API knows what key to use for signing. The `role` associated with the `role_id` filed in the token JSON will need access to this key (see [Configuration](#configuration)).

- `"port": int` (defaults to 50051)

  The port at which to start the local signer server.

### Usage

Both the `SIGNER_ENDPOINT` and `KEY_ID` can be exported in the current shell session as they are unlikely to change if running the signer locally.

```bash
export SIGNER_ENDPOINT=https://gamma.signer.cubist.dev
export KEY_ID=Key#BlsAvaIcm_0x...

TOKEN_FILE_PATH="./token.json" go run main/main.go
```

### E2E tests

#### Running Locally

To run E2E locally follow the [key creation](#key-creation) steps above to generate a new key associated with the `e2e_signer` role, and generate a session token file saved as `e2e_session.json`. After that the tests can be run via

```bash
./scripts/e2e_test.sh
```
