import unittest
import proxy
class ProxyTests(unittest.TestCase):
    def test_domain_allowlist(self):
        self.assertTrue(proxy.allowed('github.com'))
        self.assertTrue(proxy.allowed('pipelines.actions.githubusercontent.com'))
        for host in ['169.254.169.254','localhost','github.com.attacker.example','evilgithub.com','s3.amazonaws.com']:
            self.assertFalse(proxy.allowed(host),host)
if __name__=='__main__': unittest.main()
