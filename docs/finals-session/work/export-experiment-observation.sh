set -eu
cd /opt/cleanup/repo
python3 - <<'PY'
import json,pathlib
p=pathlib.Path('.build/coordinator/0bee774681fc4ddd025eecb53f5660be')
print(json.dumps({name:json.loads((p/name).read_text()) for name in ['collector.json','observations.json','finalization-ready.json']},indent=2))
PY
