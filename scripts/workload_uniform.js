import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 10,
  duration: '30s',
};

// Allow the URL to be passed via environment variable, default to localhost for local debugging
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const maxID = 1000; 
  let fromId = Math.floor(Math.random() * maxID) + 1;
  let toId = Math.floor(Math.random() * maxID) + 1;

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

  const res = http.post(`${BASE_URL}/transfers`, payload, params);

  check(res, {
    'status 201': (r) => r.status === 201,
  });

  sleep(0.01); 
}