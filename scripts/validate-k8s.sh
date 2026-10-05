#!/usr/bin/env bash
# Renders the dev overlay and checks it: schema-valid, and exactly one
# namespace (skydra-dev) with every namespaced object inside it.
set -euo pipefail
cd "$(dirname "$0")/.."

OVERLAY=k8s/overlays/dev
NAMESPACE=skydra-dev
rendered=$(kubectl kustomize "$OVERLAY")

if command -v kubeconform >/dev/null; then
  echo "$rendered" | kubeconform -strict -summary -
else
  echo "kubeconform not installed, skipping schema validation" >&2
fi

# One "kind name namespace" line per resource. kustomize output is normalized:
# top-level `kind:` and 2-space-indented `name:`/`namespace:` under `metadata:`.
objects=$(echo "$rendered" | awk '
  function flush() { if (kind != "") print kind, name, (ns == "" ? "-" : ns); kind = name = ns = ""; inmeta = 0 }
  /^---$/ { flush(); next }
  /^kind: /      { kind = $2 }
  /^metadata:/   { inmeta = 1; next }
  /^[^ ]/        { inmeta = 0 }
  inmeta && /^  name: /      { name = $2 }
  inmeta && /^  namespace: / { ns = $2 }
  END { flush() }')

namespaces=$(echo "$objects" | awk '$1 == "Namespace" {print $2}')
if [ "$namespaces" != "$NAMESPACE" ]; then
  echo "expected exactly one Namespace named $NAMESPACE, got: '$namespaces'" >&2
  exit 1
fi

stray=$(echo "$objects" | awk -v ns="$NAMESPACE" '$1 != "Namespace" && $3 != ns {print}')
if [ -n "$stray" ]; then
  echo "objects outside namespace $NAMESPACE:" >&2
  echo "$stray" >&2
  exit 1
fi
echo "k8s manifests OK: one namespace ($NAMESPACE), all objects inside it"
