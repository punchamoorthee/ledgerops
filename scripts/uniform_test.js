import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 10,
  duration: '30s',
};

export default function () {
  // Pick two random accounts between 1 and 1000
  // We use a simple helper to get random integers
  const maxID = 1000; 
  let fromId = Math.floor(Math.random() * maxID) + 1;
  let toId = Math.floor(Math.random() * maxID) + 1;

  // Ensure we don't send to self
  while (toId === fromId) {
    toId = Math.floor(Math.random() * maxID) + 1;
  }

  const payload = JSON.stringify({
    from_account_id: fromId,
    to_account_id: toId,
    amount: 10,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': `uniform-${__VU}-${__ITER}-${Date.now()}`,
    },
  };

  const res = http.post('http://localhost:8080/transfers', payload, params);

  check(res, {
    'is status 201 or 200': (r) => r.status === 201 || r.status === 200,
  });

  sleep(0.01); 
}