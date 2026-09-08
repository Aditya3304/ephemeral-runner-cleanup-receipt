#!/usr/bin/env python3
import json,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parent.parent
args=sys.argv[1:]
github='--github' in args
if github:args.remove('--github')
if args and args[0]=='status':
 states=[]
 for path in sorted((root/'.build/watchdog-state').glob('*.json')):
  value=json.loads(path.read_text())
  if value.get('kind')==('github' if github else 'local')+'-watchdog-state/v1':states.append(value['current'])
 print(json.dumps({'items':states},indent=2))
else:
 current=json.loads((root/'.build'/('github-watchdog-current.json' if github else 'watchdog-current.json')).read_text())
 raise SystemExit(subprocess.run([current['binary'],'--config',current['config'],'--once',*args],cwd=root).returncode)
