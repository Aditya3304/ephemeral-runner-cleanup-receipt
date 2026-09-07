#!/usr/bin/env python3
"""Verify that stored metadata does not disguise unavailable archived evidence."""
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

root = Path(__file__).resolve().parent.parent
def get(path):
    try:
        with urllib.request.urlopen('http://127.0.0.1:8080'+path, timeout=50) as r:
            return r.status, json.load(r)
    except urllib.error.HTTPError as e:
        return e.code, json.load(e)

status, before = get('/v1/receipts?limit=100')
assert status == 200 and before['items']
row = before['items'][0]
path = '/v1/receipts/'+row['id']+'/verification'
assert get(path)[0] == 200
subprocess.run(['docker', 'stop', 'cleanup-receipt-archive-minio-1'], check=True, capture_output=True)
try:
    status, unavailable = get(path)
    assert status == 503, (status, unavailable)
    assert get('/v1/receipts?limit=100') == (200, before)
finally:
    subprocess.run(['docker', 'start', 'cleanup-receipt-archive-minio-1'], check=True, capture_output=True)
for _ in range(30):
    status, recovered = get(path)
    if status == 200:
        break
    time.sleep(1)
else:
    raise RuntimeError('Archive did not recover')
assert recovered['signature'] == recovered['artifacts'] == 'verified'
assert recovered['receipt_sha256'] == row['receipt_object']['sha256']
report = {'receipt_id': row['id'], 'while_archive_stopped': unavailable, 'after_restart': recovered, 'metadata_unchanged': True}
(root / '.build/api-archive-fault.json').write_text(json.dumps(report, indent=2)+'\n')
print('Archive outage returned 503; restored archive reverified; metadata and exact references unchanged.')
