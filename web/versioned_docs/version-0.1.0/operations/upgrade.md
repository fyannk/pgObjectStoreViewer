---
sidebar_position: 6
---

# Upgrade and uninstall

## Upgrade

Replace the image and restart. There is no schema, no migration, no persisted
state, and no cluster-side object owned by the viewer.

Two consequences worth planning for:

- **Gap confirmation is process-local.** After a restart, previously confirmed
  gaps start again as candidates (`warning`) until a second consecutive
  complete scan re-confirms them. This is by design — confirmation is never
  persisted.
- **The first snapshot after a restart is unknown/incomplete** until the first
  scan finishes. The UI says so rather than showing zeros as facts.

Before upgrading, check the [release notes](../release-notes.md) for changes to
the frozen configuration contract. A removed or re-validated variable fails
startup rather than being silently ignored — that is deliberate, so a
misconfiguration can never become a silently wrong answer.

Rolling upgrades are safe: nothing coordinates between replicas, because
nothing is shared. Running more than one replica simply means more than one
independent scanner against the same read-only repository.

## Downgrade

Downgrading is the same operation in reverse, with one caveat: evidence
produced by a newer version may use semantics an older one does not implement.
The older version does not attempt to interpret them — unrecognized values are
`unknown` — so a downgrade loses detail rather than producing a wrong claim.

## Uninstall

```bash
kubectl delete -f deploy/kubernetes-example.yaml
```

Then revoke the provider credential or role binding.

Nothing in the object store was ever written or modified by the viewer, so
there is nothing to clean up on the repository side. If you also deployed the
example `NetworkPolicy` separately, delete it too — otherwise it keeps
selecting a workload that no longer exists, which is harmless but confusing.

## Sidecar deployments

The sidecar producer follows the Pod, not a separate lifecycle. Upgrading means
rolling the Pod that carries both containers. Because the bearer token is never
rotated in place, a token change is also a Pod roll — see
[the sidecar profile](sidecar.md#token-handling).
