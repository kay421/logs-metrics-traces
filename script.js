import http from 'k6/http';
import { sleep, check } from 'k6';
import { Counter } from 'k6/metrics';
import { htmlReport } from "https://raw.githubusercontent.com/benc-uk/k6-reporter/main/dist/bundle.js";
import { textSummary } from "https://jslib.k6.io/k6-summary/0.0.1/index.js";

// A simple counter for http requests

export const requests = new Counter('http_reqs');

// you can specify stages of your test (ramp up/down patterns) through the options object
// target is the number of VUs you are aiming for

export const options = {
  stages: [
    { target: 50, duration: '1m' },
    { target: 25, duration: '1m' },
    { target: 0, duration: '1m' },
  ],
  thresholds: {
    http_reqs: ['count < 100'],
  },
  vus: 10,
};

export function handleSummary(data) {
  return {
    "result.html": htmlReport(data),
    stdout: textSummary(data, { indent: " ", enableColors: true }),
  };
}

export default function () {
  http.get('http://localhost:8080/random-sleep');
  http.get('http://localhost:8080/blogs');
  sleep(1);

}

