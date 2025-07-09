# Cubist Signer

A proxy to be run alongside an
[`avalanchego`](https://github.com/ava-labs/avalanchego) validator, used for all
bls signatures (currently used for peer handshakes and ICM message signatures).
This service runs a [gRPC](https://grpc.io/) server, implementing the
[`signer.proto` service definition](https://github.com/ava-labs/avalanchego/blob/master/proto/signer/signer.proto).
When the `gRPC` endpoints are hit, they make subsequent requests to the
[Cubist-API](https://signer-docs.cubist.dev/api)

## Building

The `api/` directory contains generated code from the
[Cubist OpenAPI specification](https://raw.githubusercontent.com/cubist-labs/CubeSigner-TypeScript-SDK/main/packages/sdk/spec/openapi.json).
We use the [`spec/get-schemas.go`] script to filter the API-spec for the three
endpoints that we care about as well as all the schemas that those endpoints
use. The filtered Open-API specification is output to
`spec/filtered-openapi.json`.

If there are changes in `spec/filtered-openapi.json`, you _must_ run
`go generate ./signerserver` to re-generate the client code in the `api/`
directory.

## Testing

Currently, the only way to test the code is using
[this PR](https://github.com/ava-labs/avalanchego/pull/3725) where you have to
set the `--staking-rpc-signer=127.0.0.1:50051` configuration flag. You must
first start this application before starting the `avalanchego` node.

## Running

### Key Creation

To run the `cubist-signer` locally, you will need a
[`CubeSigner`](https://github.com/cubist-partners/CubeSigner/) application
(sorry for the similar names). If you need an invite to see the repository,
please reach out to someone on the @ava-labs/interop team. Once installed, you
will need to set up the `CubeSigner` and login following
[the Getting Started instructions](https://signer-docs.cubist.dev/getting-started)
(the docs are password protected, the password should be in the `CubeSigner`
README). After, you will need to create a `role` with the following command:

```shell
cs role create --role-name bls_signer
```

Then create a key:

```shell
cs keys create --key-type=bls-ava-icm
```

Next, set the signing policy on the key:

```shell
# you can find the <key_id> using `cs keys list`
cs key set-policy --key-id <key_id> --policy '"AllowRawBlobSigning"'
```

After, you need to add the key to the role

```shell
# you can either use the full <role_id> or use the <role_name> ("bls_signer" from above, for example)
cs role add-key --role-id <role_id> --key-id <key_id>
```

Finally, you can create a token file associated with the role that you created:

```shell
cs token create --role-id <role_id> > <path_to_token_file>.json
```

### Configuration

Below is a list of configuration options that can be set via a JSON config file passed in via `--config-file` flag or set through environment variables or flags. To get the environment variable corresponding to the key uppercase the key and change the delimiter from "-" to "_". The following precedence order is used, with each item taking precedence over items below it:
1. Flags
2. Environment variables
3. Config file

- `"token-file-path": string` (required)

  This is the relative path (absolute paths also work) to the token file, we
  created from the last step above.

  The `refresh-token` (part of the JSON output of `cs token create`) has a very
  short TTL by default, so you must start the signer (this repo) before it
  expires. Once started, your `<path_to_token>.json` file will be continuously
  refreshed as needed. To change any of the default token parameters, see
  `cs token create --help`.

- `"signer-endpoint": string` (required)

  This is the Cubist-API endpoint.

- `"key-id": string` (required)

  The `cubist-signer` (this repo) can only use one key at a time as an
  `avalanchego` validator is only meant to have a single BLS signing key.
  Specifying the `KEY_ID` is how the Cubist-API knows what key to use for
  signing. The `role` associated with the `role_id` filed in the token JSON will
  need access to this key (see [Configuration](#configuration))

- `"port": int` (defaults to 50051)

  Port at which to start the local signer server.

### Usage

Both the `SIGNER_ENDPOINT` and `KEY_ID` can be exported in your current shell
session as they are unlikely to change if you running the signer locally.

```bash
export SIGNER_ENDPOINT=https://gamma.signer.cubist.dev
export KEY_ID=Key#BlsAvaIcm_0x...

TOKEN_FILE_PATH="./token.json" go run main/main.go
```

### E2E tests

#### Running Locally
To run E2E locally follow the steps in [Key Creation](#key-creation) above to generate a new key associated with the `e2e_signer` role and generate a session token file saved as `e2e_session.json`. After that the tests can be run via 

```bash
./scripts/e2e_test.sh
```

#### CI
The base64 encoded session token is stored in Github secrets. It currently expires on 6/05/2026. When it expires, a new token can be generated and stored there.
