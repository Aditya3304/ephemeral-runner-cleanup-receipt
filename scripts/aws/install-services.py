#!/usr/bin/env python3
import pathlib, subprocess, json, ipaddress, shutil
root=pathlib.Path(__file__).resolve().parents[2]
revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
network=json.loads(subprocess.check_output(['docker','network','inspect','kind'],text=True))[0]
proxy=next(c['Gateway'] for c in network['IPAM']['Config'] if ipaddress.ip_address(c['Gateway']).version==4)
public_proxy=pathlib.Path('/usr/local/lib/cleanup-github-proxy.py')
shutil.copyfile(root/'infra/github/proxy.py',public_proxy)
public_proxy.chmod(0o644)
units={
'cleanup-github-broker.service':f'''[Unit]
Description=Credential-free GitHub request queue reached only through SSM
After=network-online.target
[Service]
ExecStart=/usr/bin/python3 {root}/infra/github/broker.py
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
Restart=on-failure
UMask=0077
[Install]
WantedBy=multi-user.target
''',
'cleanup-archive-sessions.service':f'''[Unit]
Description=Rotate separated AWS archive sessions
After=docker.service network-online.target
[Service]
Type=oneshot
WorkingDirectory={root}
ExecStart=/usr/bin/python3 {root}/scripts/aws/sessions.py
UMask=0077
''',
'cleanup-archive-sessions.timer':'''[Timer]
OnBootSec=30s
OnUnitActiveSec=15min
[Install]
WantedBy=timers.target
''',
'cleanup-github-proxy.service':f'''[Unit]
Description=Allowlisted GitHub HTTPS proxy
After=docker.service network-online.target
[Service]
WorkingDirectory=/
ExecStart=/usr/bin/python3 {public_proxy} --bind {proxy}
User=nobody
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
Restart=on-failure
MemoryMax=96M
TasksMax=64
[Install]
WantedBy=multi-user.target
''',
'cleanup-github.service':f'''[Unit]
Description=Source-approved ephemeral GitHub runner coordinator
After=cleanup-github-proxy.service cleanup-github-broker.service docker.service network-online.target
[Service]
WorkingDirectory={root}
ExecStart={root}/.build/githubci --revision {revision} --proxy-ip {proxy}
UMask=0077
Restart=on-failure
RestartSec=30
KillMode=process
TimeoutStopSec=180
[Install]
WantedBy=multi-user.target
'''}
for name,text in units.items(): pathlib.Path('/etc/systemd/system',name).write_text(text)
subprocess.run(['systemctl','daemon-reload'],check=True)
subprocess.run(['systemctl','enable','--now','cleanup-archive-sessions.timer','cleanup-github-proxy.service','cleanup-github-broker.service','cleanup-github.service'],check=True)
