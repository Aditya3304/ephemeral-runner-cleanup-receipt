import subprocess,unittest,urllib.error
from read_retry import read_retry
class RetryTests(unittest.TestCase):
    def test_recovers_from_the_reported_timeout(self):
        calls=[]
        def action():
            calls.append(1)
            if len(calls)==1:raise subprocess.CalledProcessError(1,['gh','api'],stderr='dial tcp 20.207.73.85:443: i/o timeout')
            return {'status':'completed'}
        self.assertEqual(read_retry('test',action,sleep=lambda _:None),{'status':'completed'})
        self.assertEqual(len(calls),2)
    def test_does_not_retry_permission_denial(self):
        calls=[]
        def action():
            calls.append(1)
            raise subprocess.CalledProcessError(1,['gh','api'],stderr='HTTP 403 forbidden')
        with self.assertRaises(subprocess.CalledProcessError):read_retry('test',action,sleep=lambda _:None)
        self.assertEqual(len(calls),1)
    def test_stops_after_bounded_retries(self):
        calls=[]
        def action():
            calls.append(1)
            raise urllib.error.URLError('timed out')
        with self.assertRaises(urllib.error.URLError):read_retry('test',action,attempts=3,sleep=lambda _:None)
        self.assertEqual(len(calls),3)
if __name__=='__main__':unittest.main()
