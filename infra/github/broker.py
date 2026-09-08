#!/usr/bin/env python3
"""Cloud loopback queue. The laptop supplies GitHub responses over an SSM tunnel.

No administrative GitHub credential reaches this service. JIT responses are
single-use runner credentials and exist only in memory for the pending request.
"""
import http.server, json, queue, secrets, threading
pending=queue.Queue(maxsize=8)
active={}
lock=threading.Lock()
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self,*args): pass
    def send(self,status,body):
        raw=json.dumps(body).encode();self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers()
        try:self.wfile.write(raw)
        except OSError:pass
    def handle_request(self):
        n=int(self.headers.get('Content-Length','0'))
        if n<0 or n>2*1024*1024:return self.send(413,{})
        body=json.loads(self.rfile.read(n)) if n else None
        if self.path=='/bridge/poll' and self.command=='GET':
            try:item=pending.get(timeout=20)
            except queue.Empty:return self.send(200,{})
            return self.send(200,item)
        if self.path.startswith('/bridge/response/') and self.command=='POST':
            with lock: state=active.get(self.path.rsplit('/',1)[-1])
            if not state:return self.send(410,{})
            state['response']=body;state['event'].set();return self.send(200,{})
        if not self.path.startswith('/api/repos/Aditya3304/ephemeral-runner-cleanup-receipt/') or self.command not in ['GET','POST','DELETE']:return self.send(403,{})
        ident=secrets.token_hex(16);state={'event':threading.Event()}
        with lock:active[ident]=state
        try:
            pending.put_nowait({'id':ident,'method':self.command,'path':self.path[4:],'body':body})
            if not state['event'].wait(50):return self.send(503,{})
            response=state['response'];return self.send(int(response['status']),response['body'])
        except queue.Full:return self.send(503,{})
        finally:
            with lock:active.pop(ident,None)
    do_GET=handle_request
    do_POST=handle_request
    do_DELETE=handle_request
if __name__=='__main__':http.server.ThreadingHTTPServer(('127.0.0.1',8123),Handler).serve_forever()
