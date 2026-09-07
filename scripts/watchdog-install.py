#!/usr/bin/env python3
"""Install a pinned local operator binary; never execute checkout scripts at runtime."""
import hashlib,json,os,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parent.parent
build=root/'.build'

if '--service' in sys.argv:
 # Terminal groups can differ from a user manager started before Docker setup.
 check=subprocess.run(['systemd-run','--user','--wait','--pipe','--property=NoNewPrivileges=yes','/usr/bin/docker','info','--format','{{.ServerVersion}}'],capture_output=True,timeout=30)
 if check.returncode:raise SystemExit('The WSL user service cannot reach Docker. Start Docker Desktop; if terminal Docker works, refresh the WSL user manager group membership. See docs/watchdog.md.')
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
binary=build/'watchdog'
policy=build/'finalizer-config.json'
images={k:(build/v).read_text().strip() for k,v in [('api_image','api-image-id'),('finalizer_image','finalizer-image-id')]}
for kind,image in images.items():
 import re
 if not re.fullmatch(r'sha256:[a-f0-9]{64}',image):raise SystemExit('Invalid pinned image')
 subprocess.run(['docker','image','inspect',image],check=True,stdout=subprocess.DEVNULL)
 # A retained tag keeps Docker's local manifest reachable after api:local moves.
 # Execution still uses the exact content digest recorded in the installation.
 subprocess.run(['docker','tag',image,'cleanup-receipt/watchdog-'+kind.replace('_image','')+':'+image[7:]],check=True)
fingerprint=hashlib.sha256((digest(binary)+digest(policy)+json.dumps(images,sort_keys=True)).encode()).hexdigest()
target=build/'watchdog-install'/fingerprint
target.mkdir(parents=True,exist_ok=True)
for source,name in [(binary,'watchdog'),(policy,'finalizer.json')]:
 dest=target/name
 if dest.exists() and digest(dest)!=digest(source):raise SystemExit('Installed operator artifact changed; investigate before reinstalling')
 if not dest.exists():dest.write_bytes(source.read_bytes())
os.chmod(target/'watchdog',0o700)
config={'root':str(root),'state':str(build/'watchdog-state'),'finalizer_config':str(target/'finalizer.json'),**images,'files':{str(target/name):digest(target/name) for name in ['watchdog','finalizer.json']}}
configpath=target/'config.json'
expected=json.dumps(config,indent=2)+'\n'
if configpath.exists() and configpath.read_text()!=expected:raise SystemExit('Installed configuration differs')
configpath.write_text(expected)
(build/'watchdog-current.json').write_text(json.dumps({'binary':str(target/'watchdog'),'config':str(configpath)},indent=2)+'\n')
if '--service' in sys.argv:
 unitdir=pathlib.Path.home()/'.config/systemd/user'
 unitdir.mkdir(parents=True,exist_ok=True)
 unit=unitdir/'cleanup-receipt-watchdog.service'
 if unit.exists() and not unit.read_text().startswith('# Managed by cleanup-receipt watchdog'):raise SystemExit('Refusing to replace an unrelated unit')
 def quote(s):return '"'+str(s).replace('\\','\\\\').replace('"','\\"').replace('%','%%')+'"'
 unit.write_text('''# Managed by cleanup-receipt watchdog
[Unit]
Description=Local cleanup receipt recovery watchdog

[Service]
Type=simple
ExecStart='''+quote(target/'watchdog')+' --config '+quote(configpath)+'''
Restart=on-failure
RestartSec=30
TimeoutStopSec=45
MemoryMax=768M
TasksMax=96
NoNewPrivileges=true
UMask=0077
Environment=GOMEMLIMIT=512MiB GOMAXPROCS=2

[Install]
WantedBy=default.target
''')
 subprocess.run(['systemctl','--user','daemon-reload'],check=True)
 subprocess.run(['systemctl','--user','enable','cleanup-receipt-watchdog.service'],check=True)
 subprocess.run(['systemctl','--user','restart','cleanup-receipt-watchdog.service'],check=True)
 print('Local watchdog service enabled. It reconciles at startup and every five minutes.')
else:print('Pinned watchdog installed for manual validation; service not yet enabled.')
