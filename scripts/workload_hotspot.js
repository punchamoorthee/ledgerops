import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 50,
  duration: '30s',
};

// Allow the URL to be passed via environment variable
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
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
      'Idempotency-Key': `hotspot-${__VU}-${__ITER}-${Date.now()}`,
    },
  };

  const res = http.post(`${BASE_URL}/transfers`, payload, params);

  check(res, {
    'status 201 or 200': (r) => r.status === 201 || r.status === 200,
  });

  sleep(0.01);
}