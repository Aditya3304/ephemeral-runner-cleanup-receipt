import assert from 'node:assert/strict';
import test from 'node:test';
import {summarize} from './order-export.mjs';

test('known order fixture totals 4999 cents', () => {
  assert.deepEqual(summarize([{quantity: 2, unitCents: 1250}, {quantity: 1, unitCents: 999}, {quantity: 3, unitCents: 500}]), {orders: 3, totalCents: 4999});
});
test('reject invalid quantities, prices and unsafe totals', () => {
  for (const order of [{quantity: 0, unitCents: 100}, {quantity: 1.5, unitCents: 100}, {quantity: 1, unitCents: -1}, {quantity: 2, unitCents: Number.MAX_SAFE_INTEGER}]) {
    assert.throws(() => summarize([order]));
  }
});
