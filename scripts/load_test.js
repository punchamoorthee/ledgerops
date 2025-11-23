import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 10,
  duration: '30s',
};

export default function () {
  // Toggle direction based on iteration number to keep balances roughly equal
  // If iteration is even: 1 -> 2. If odd: 2 -> 1.
  const isEven = __ITER % 2 === 0;
  const fromId = isEven ? 1 : 2;
  const toId = isEven ? 2 : 1;

  const payload = JSON.stringify({
    from_account_id: fromId,
    to_account_id: toId,
    amount: 10,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': `key-${__VU}-${__ITER}-${Date.now()}`, // Added Date.now() to ensure uniqueness across runs
    },
  };

  const res = http.post('http://localhost:8080/transfers', payload, params);

  check(res, {
    'is status 201 or 200': (r) => r.status === 201 || r.status === 200,
  });

  // Add a check for Insufficient Funds to distinguish it from server crashes
  if (res.status === 422) {
      console.log("Hit insufficient funds!");
  }

  sleep(0.01); // Reduced sleep to push the system harder
}