#!/usr/bin/env python3
"""CONNECT-only GitHub egress proxy. No credentials, API, or filesystem access."""
import argparse
import ipaddress
import selectors
import socket
import socketserver
import time

EXACT = {'github.com', 'api.github.com', 'codeload.github.com', 'objects.githubusercontent.com',
         'github-releases.githubusercontent.com', 'github-registry-files.githubusercontent.com'}
SUFFIX = ('.actions.githubusercontent.com', '.githubusercontent.com', '.blob.core.windows.net')

def allowed(host):
    host=host.lower()
    return len(host)<=253 and (host in EXACT or any(host.endswith(s) and host!=s[1:] for s in SUFFIX))

def resolve_public(host):
    addresses=socket.getaddrinfo(host,443,type=socket.SOCK_STREAM)
    if not addresses or any(not ipaddress.ip_address(a[4][0]).is_global for a in addresses):
        raise ValueError('Non-public destination refused')
    return addresses

class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(10)
        line=self.rfile.readline(4097)
        if len(line)>4096:return
        try:
            method,target,version=line.decode('ascii').strip().split(' ')
            host,port=target.rsplit(':',1)
            if method!='CONNECT' or port!='443' or not allowed(host):raise ValueError()
            total=0
            while True:
                header=self.rfile.readline(4097); total+=len(header)
                if total>16384 or not header:raise ValueError()
                if header==b'\r\n':break
            addresses=resolve_public(host)
            upstream=None
            for family,kind,proto,_,addr in addresses:
                try:
                    upstream=socket.socket(family,kind,proto); upstream.settimeout(10); upstream.connect(addr); break
                except OSError:
                    if upstream:upstream.close()
                    upstream=None
            if upstream is None:raise OSError()
        except (ValueError,UnicodeError,OSError):
            self.wfile.write(b'HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n'); return
        with upstream,selectors.DefaultSelector() as sel:
            self.wfile.write(b'HTTP/1.1 200 Connection Established\r\n\r\n'); self.wfile.flush()
            sel.register(self.connection,selectors.EVENT_READ,upstream)
            sel.register(upstream,selectors.EVENT_READ,self.connection)
            end=time.monotonic()+900
            while time.monotonic()<end:
                ready=sel.select(30)
                if not ready:continue
                for key,_ in ready:
                    try:
                        data=key.fileobj.recv(65536)
                        if not data:return
                        key.data.sendall(data)
                    except OSError:return

class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address=True
    daemon_threads=True
    request_queue_size=32

if __name__=='__main__':
    parser=argparse.ArgumentParser(); parser.add_argument('--bind',required=True); a=parser.parse_args()
    if not ipaddress.ip_address(a.bind).is_private:raise SystemExit('Bind only to the private kind bridge')
    with Server((a.bind,3128),Handler) as server:server.serve_forever()
