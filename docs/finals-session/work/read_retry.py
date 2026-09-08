"""Bounded retries for read-only checks. Never wrap a job dispatch or mutation."""
import subprocess,time,urllib.error

def transient(error):
    if isinstance(error,urllib.error.HTTPError):
        return error.code in (429,500,502,503,504)
    if isinstance(error,(urllib.error.URLError,TimeoutError,ConnectionError,subprocess.TimeoutExpired)):
        return True
    if isinstance(error,subprocess.CalledProcessError):
        detail=error.stderr or ''
        if isinstance(detail,bytes):detail=detail.decode(errors='replace')
        return any(term in detail.lower() for term in ['i/o timeout','tls handshake timeout','connection reset','connection refused','temporary failure','network is unreachable','unexpected eof','http 429','http 500','http 502','http 503','http 504','could not connect to the endpoint'])
    return False

def read_retry(label,action,attempts=5,sleep=time.sleep):
    for attempt in range(attempts):
        try:return action()
        except Exception as error:
            if not transient(error) or attempt+1==attempts:raise
            seconds=min(2**(attempt+1),15)
            print(f'{label}: temporary connection/service error; retry {attempt+2}/{attempts} in {seconds}s. No job is being re-dispatched.',flush=True)
            sleep(seconds)
