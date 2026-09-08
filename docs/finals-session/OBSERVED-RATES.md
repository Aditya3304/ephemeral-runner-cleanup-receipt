# Observed controlled-demo results

Source: `outputs/walkthrough-execution/runs.json`, archived 8 September 2026.
These rates compare labeled controlled scenarios with recorded cleanup verdicts.

The latest four runs were 34220096575 (normal/pass), 34220301677
(test-failure/pass), 34220482427 (cleanup-blocked/fail), and 34220681769
(normal/pass). Their exported observer findings agree: empty workspace in the
three expected-clean cases and residue in the blocked case.

With cleanup failure defined as the positive detection:

- Latest batch: false alarms 0/3 expected-clean cases; missed failures 0/1 blocked case.
- All 11 labeled walkthrough runs: false alarms 0/8 expected-clean cases; missed failures 0/3 blocked cases.

All these observed rates are 0%. They are small controlled samples, not estimates
establishing a zero production error rate or broad failure-mode coverage.
Application-test failure followed by a clean receipt is expected behavior.
The broader dashboard's earlier partial receipt is inconclusive, not a confirmed
false alarm or successful classification. Broader dashboard totals are not used
as the denominator without labeled expected outcomes.

Signature validity protects evidence integrity; factual accuracy still depends
on the observer and stated trust assumptions.
