# Hard-Binding Example

This example demonstrates the mandatory v2 pattern: the application cannot read its real runtime config until the SDK has loaded a valid machine-bound lease and `Unseal` succeeds.

## Files

- `main.go`: sample application startup.
- `sealbox/main.go`: offline provisioning helper that seals a plaintext config with a specific lease + lease signature.

## Provisioning Flow

1. Obtain a real lease and `lease_signature` from `/api/v1/verify` or `/api/v1/heartbeat` for the target machine.
2. Save the canonical lease JSON to `lease.json`.
3. Save the plaintext runtime config to `config.json`.
4. Generate the sealed blob:

```bash
go run ./examples/hardbinding/sealbox \
  -lease ./lease.json \
  -signature "$LEASE_SIGNATURE_B64" \
  -config ./config.json \
  -out ./examples/hardbinding/config.sealed
```

5. Place the matching `public_key.pem` next to `main.go`, then run the sample app with:

```bash
export GUARD_SERVER_URL=https://guard.example.com
export GUARD_LICENSE_KEY=XXXX-XXXX
export GUARD_PROJECT_SLUG=my-project
export GUARD_COMPONENT_SLUG=backend
export GUARD_PINNED_SPKI=base64-spki-primary,base64-spki-rotation
go run ./examples/hardbinding
```

## Why This Defeats `Check() -> nil`

The app does not trust `Check()` alone. It exits unless:

1. the SDK has a valid signed lease for the current machine, and
2. `guard.Unseal(config.sealed)` returns the real config bytes.

If an attacker stubs `Check()` but cannot produce a valid lease-derived secret, `Unseal` fails and the app never gets its DSN / API base URL.

## Mandatory Integration Rule

Commercial apps should move at least one business-critical secret or routing document behind `Unseal`, and should use `FeatureToken` for downstream feature proofs or signed internal requests. Without this, the SDK remains advisory even if the license state is correct.
