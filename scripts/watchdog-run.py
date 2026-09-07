#!/usr/bin/env python3
import json,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parent.parent
args=sys.argv[1:]
if args and args[0]=='status':
 states=[]
 for path in sorted((root/'.build/watchdog-state').glob('*.json')):
  value=json.loads(path.read_text())
  if value.get('kind')=='local-watchdog-state/v1':states.append(value['current'])
 print(json.dumps({'items':states},indent=2))
else:
 current=json.loads((root/'.build/watchdog-current.json').read_text())
 raise SystemExit(subprocess.run([current['binary'],'--config',current['config'],'--once',*args],cwd=root).returncode)
