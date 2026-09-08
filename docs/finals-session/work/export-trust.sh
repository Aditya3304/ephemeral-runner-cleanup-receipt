python3 - <<'PY'
import base64,json,pathlib
root=pathlib.Path('/opt/cleanup/repo/.build/finalizer-trust')
print(json.dumps({name:base64.b64encode((root/name).read_bytes()).decode() for name in ['trusted-root.json','signing-config.json','issuer-ca.crt']}))
PY
