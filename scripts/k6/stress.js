import exec from 'k6/execution';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE_URL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/+$/, '');
const K6_USER = __ENV.K6_USER || '';
const K6_LEADER = __ENV.K6_LEADER || '';
const K6_PASSWORD = __ENV.K6_PASSWORD || '';
const SUBMITTER_PASSWORD = __ENV.K6_USER_PASSWORD || K6_PASSWORD;
const LEADER_PASSWORD = __ENV.K6_LEADER_PASSWORD || K6_PASSWORD;
const K6_DB_A = __ENV.K6_DB_A || '';
const K6_DB_B = __ENV.K6_DB_B || '';
const SUMMARY_JSON = __ENV.K6_SUMMARY_JSON || '';
const STAGE_SECONDS = envInt('K6_STAGE_SECONDS', 30, 5);
const JOB_TIMEOUT_SECONDS = Math.min(envInt('K6_JOB_TIMEOUT_SECONDS', 20, 1), STAGE_SECONDS - 1);
const JOB_TIMEOUT_MS = JOB_TIMEOUT_SECONDS * 1000;
const POLL_INTERVAL_SECONDS = 0.5;
const API_THINK_SECONDS = 0.25;
const STRESS_SQL = 'SELECT 1';

const STAGES = [
    { name: 'baseline', apiVus: 5, jobVus: 1 },
    { name: 'ramp-1', apiVus: 10, jobVus: 1 },
    { name: 'ramp-2', apiVus: 20, jobVus: 2 },
    { name: 'peak', apiVus: 30, jobVus: 3 },
    { name: 'cool-down', apiVus: 10, jobVus: 1 },
    { name: 'recovery', apiVus: 5, jobVus: 1 },
];

const STAGE_BY_NAME = {};
let jobOffset = 0;
for (const stage of STAGES) {
    stage.jobOffset = jobOffset;
    jobOffset += stage.jobVus;
    STAGE_BY_NAME[stage.name] = stage;
}

const TOTAL_JOB_VUS = jobOffset;
const JOB_SLOTS = new SharedArray('k6JobSlots', function () {
    const slots = [];
    for (let i = 0; i < TOTAL_JOB_VUS; i++) {
        slots.push({ index: i, db: i % 2 === 0 ? 'a' : 'b' });
    }
    return slots;
});

const TERMINAL = {
    success: true,
    failed: true,
    partial: true,
    uncertain: true,
    partially_reverted: true,
    cancelled: true,
};

const apiResponseOk = new Rate('api_response_ok');
const jobSubmitAccepted = new Rate('job_submit_accepted');
const jobsSuccess = new Rate('jobs_success');
const jobsCompleted = new Counter('jobs_completed');
const jobsTimeout = new Counter('jobs_timeout');
const jobQueueWait = new Trend('job_queue_wait_ms');
const jobDuration = new Trend('job_duration_ms');

export const options = {
    systemTags: ['method', 'status', 'name', 'error_code'],
    summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'max'],
    thresholds: buildThresholds(),
    scenarios: buildScenarios(),
};

function envInt(name, fallback, min) {
    const value = Number.parseInt(__ENV[name] || '', 10);
    return Number.isInteger(value) && value >= min ? value : fallback;
}

function buildScenarios() {
    const scenarios = {};
    for (let i = 0; i < STAGES.length; i++) {
        const stage = STAGES[i];
        const startTime = (i * STAGE_SECONDS) + 's';
        scenarios[stage.name + '_api'] = {
            executor: 'constant-vus',
            vus: stage.apiVus,
            duration: STAGE_SECONDS + 's',
            startTime: startTime,
            gracefulStop: '1s',
            exec: 'apiRead',
            tags: { stage: stage.name, op: 'api-read' },
        };
        scenarios[stage.name + '_job'] = {
            executor: 'constant-vus',
            vus: stage.jobVus,
            duration: STAGE_SECONDS + 's',
            startTime: startTime,
            gracefulStop: '1s',
            exec: 'submitJob',
            tags: { stage: stage.name, op: 'job-submit' },
        };
    }
    return scenarios;
}

function buildThresholds() {
    const thresholds = {
        http_req_duration: ['p(95)<800'],
        'http_req_duration{op:api-read}': ['p(95)<500'],
        http_req_failed: ['rate<0.01'],
        'http_req_failed{op:api-read}': ['rate<0.01'],
        job_submit_accepted: ['rate>0.95'],
        job_queue_wait_ms: ['p(95)<5000'],
        jobs_success: ['rate>0.95'],
    };
    for (const stage of STAGES) {
        thresholds['http_req_duration{op:api-read,stage:' + stage.name + '}'] = ['p(95)<500'];
        thresholds['http_req_failed{op:api-read,stage:' + stage.name + '}'] = ['rate<0.01'];
        thresholds['http_reqs{op:api-read,stage:' + stage.name + '}'] = ['count>=0'];
        thresholds['job_queue_wait_ms{stage:' + stage.name + '}'] = ['p(95)<5000'];
        thresholds['job_duration_ms{stage:' + stage.name + '}'] = ['p(95)<60000'];
        thresholds['jobs_completed{stage:' + stage.name + '}'] = ['count>=0'];
        thresholds['jobs_success{stage:' + stage.name + '}'] = ['rate>0.95'];
    }
    return thresholds;
}

function parseJSON(res) {
    try {
        return res.json();
    } catch (err) {
        return null;
    }
}

function headerValue(headers, name) {
    if (!headers) {
        return null;
    }
    if (headers[name]) {
        return headers[name];
    }
    const target = name.toLowerCase();
    for (const key of Object.keys(headers)) {
        if (key.toLowerCase() === target) {
            return headers[key];
        }
    }
    return null;
}

function cookieHeader(res) {
    const raw = headerValue(res.headers, 'Set-Cookie');
    if (!raw) {
        return '';
    }
    const values = Array.isArray(raw) ? raw : [raw];
    const cookies = [];
    for (const value of values) {
        const pair = String(value).split(';')[0].trim();
        if (pair) {
            cookies.push(pair);
        }
    }
    return cookies.join('; ');
}

function login(username, password) {
    const res = http.post(BASE_URL + '/api/login', JSON.stringify({ username: username, password: password }), {
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        tags: { stage: 'setup', op: 'login' },
    });
    expectStatus(res, 200, 'login ' + username);
    const body = parseJSON(res);
    const csrf = body && body.csrf_token ? body.csrf_token : '';
    const cookie = cookieHeader(res);
    if (!csrf || !cookie) {
        throw new Error('login ' + username + ' tidak mengembalikan sesi lengkap');
    }
    return { csrf: csrf, cookie: cookie };
}

function request(method, url, body, session, tags) {
    const requestTags = Object.assign({ stage: 'setup', op: 'request' }, tags || {});
    if (!requestTags.name) {
        requestTags.name = requestTags.op;
    }
    const headers = { Accept: 'application/json' };
    if (session && session.cookie) {
        headers.Cookie = session.cookie;
    }
    if (body !== null && body !== undefined) {
        headers['Content-Type'] = 'application/json';
    }
    if (method !== 'GET' && session && session.csrf) {
        headers['X-CSRF-Token'] = session.csrf;
    }
    const params = { headers: headers, tags: requestTags };
    let res = http.request(method, url, body === null || body === undefined ? null : JSON.stringify(body), params);
    if (res.status === 403 && session) {
        const err = parseJSON(res);
        if (err && err.error && err.error.code === 'csrf_invalid') {
            const refresh = http.get(BASE_URL + '/api/csrf', {
                headers: { Accept: 'application/json', Cookie: session.cookie },
                tags: Object.assign({}, requestTags, { op: 'csrf-refresh' }),
            });
            const refreshed = parseJSON(refresh);
            if (refresh.status === 200 && refreshed && refreshed.csrf_token) {
                session.csrf = refreshed.csrf_token;
                if (method !== 'GET') {
                    headers['X-CSRF-Token'] = session.csrf;
                }
                res = http.request(method, url, body === null || body === undefined ? null : JSON.stringify(body), params);
            }
        }
    }
    return res;
}

function expectStatus(res, expected, context) {
    if (res.status === expected) {
        return;
    }
    const body = parseJSON(res);
    const code = body && body.error && body.error.code ? body.error.code : '-';
    throw new Error(context + ': HTTP ' + res.status + ', code=' + code);
}

function pickDatabase(items, envID, label) {
    let selected = null;
    if (envID) {
        for (const item of items) {
            if (String(item.id) === envID) {
                selected = item;
                break;
            }
        }
        if (!selected) {
            throw new Error('K6_DB_' + label.toUpperCase() + ' tidak ditemukan dalam daftar database');
        }
    } else {
        for (const item of items) {
            if (item.level === 'edit') {
                selected = item;
                break;
            }
        }
    }
    if (!selected) {
        throw new Error('Tidak ada database dengan akses edit untuk K6_DB_' + label.toUpperCase());
    }
    if (selected.level !== 'edit') {
        throw new Error('Database K6_DB_' + label.toUpperCase() + ' harus punya akses edit');
    }
    return { id: String(selected.id), label: label, name: selected.name };
}

function withTable(db, session) {
    const res = request('GET', BASE_URL + '/api/databases/' + db.id, null, session, {
        stage: 'setup',
        op: 'setup-db-detail',
        db: db.label,
    });
    expectStatus(res, 200, 'detail database ' + db.label);
    const body = parseJSON(res);
    const schemas = body && Array.isArray(body.schemas) ? body.schemas : [];
    for (const schema of schemas) {
        const tables = schema && Array.isArray(schema.tables) ? schema.tables : [];
        for (const table of tables) {
            return {
                id: db.id,
                label: db.label,
                name: db.name,
                schema: schema.name,
                table: table.name,
            };
        }
    }
    throw new Error('Database ' + db.label + ' tidak punya tabel yang bisa dibaca');
}

function createMigrations(session, a, b) {
    const migrations = [];
    for (const slot of JOB_SLOTS) {
        const db = slot.db === 'a' ? a : b;
        const tags = { stage: 'setup', op: 'setup-create-migration', db: db.label };
        const created = request('POST', BASE_URL + '/api/databases/' + db.id + '/migrations', {
            title: 'k6 stress ' + ('0' + (slot.index + 1)).slice(-2),
            sql_up: STRESS_SQL,
            sql_down: '',
            is_irreversible: false,
            irreversible_reason: '',
            is_batch_mode: false,
            batch_column: '',
            batch_size: 0,
        }, session, tags);
        expectStatus(created, 201, 'create migration ' + slot.index);
        const createdBody = parseJSON(created);
        const migrationID = createdBody && createdBody.id ? String(createdBody.id) : '';
        if (!migrationID) {
            throw new Error('create migration ' + slot.index + ' tidak mengembalikan id');
        }
        const submitted = request('POST', BASE_URL + '/api/migrations/' + migrationID + '/submit', {}, session,
            Object.assign({}, tags, { op: 'setup-submit-migration' }));
        expectStatus(submitted, 200, 'submit migration ' + migrationID);
        migrations.push(migrationID);
    }
    return migrations;
}

function approveMigrations(session, migrations) {
    for (let i = 0; i < migrations.length; i++) {
        const res = request('POST', BASE_URL + '/api/migrations/' + migrations[i] + '/approve', {
            revision_number: 1,
        }, session, { stage: 'setup', op: 'setup-approve-migration' });
        expectStatus(res, 200, 'approve migration ' + migrations[i]);
        const body = parseJSON(res);
        if (!body || body.state !== 'approved') {
            throw new Error('migration ' + migrations[i] + ' belum approved: ' + (body && body.state ? body.state : '-'));
        }
    }
}

export function setup() {
    if (!K6_USER || !K6_LEADER || !SUBMITTER_PASSWORD || !LEADER_PASSWORD) {
        throw new Error('K6_USER, K6_LEADER, dan password keduanya wajib diisi');
    }
    const submitter = login(K6_USER, SUBMITTER_PASSWORD);
    const leader = login(K6_LEADER, LEADER_PASSWORD);
    const listRes = request('GET', BASE_URL + '/api/databases?page=1&page_size=100', null, submitter, {
        stage: 'setup',
        op: 'setup-db-list',
    });
    expectStatus(listRes, 200, 'list database');
    const listBody = parseJSON(listRes);
    const items = listBody && Array.isArray(listBody.items) ? listBody.items : [];
    const a = pickDatabase(items, K6_DB_A, 'a');
    const others = [];
    for (const item of items) {
        if (String(item.id) !== a.id) {
            others.push(item);
        }
    }
    const b = pickDatabase(others, K6_DB_B, 'b');
    const databases = { a: withTable(a, submitter), b: withTable(b, submitter) };
    const migrations = createMigrations(submitter, databases.a, databases.b);
    approveMigrations(leader, migrations);
    return { submitter: { csrf: submitter.csrf, cookie: submitter.cookie }, dbs: databases, migrations: migrations };
}

function apiRead(data) {
    const stage = STAGE_BY_NAME[exec.scenario.name];
    const iteration = exec.scenario.iterationInTest;
    const db = iteration % 2 === 0 ? data.dbs.a : data.dbs.b;
    const opIndex = iteration % 3;
    const tags = { stage: stage.name, op: 'api-read', db: db.label, name: 'api-read' };
    let res = null;
    let ok = false;
    if (opIndex === 0) {
        tags.name = 'api-databases';
        res = request('GET', BASE_URL + '/api/databases?page=1&page_size=100', null, data.submitter, tags);
        const body = parseJSON(res);
        ok = res.status === 200 && body !== null && Array.isArray(body.items);
        check(res, { 'api database list ok': function () { return ok; } }, tags);
    } else if (opIndex === 1) {
        tags.name = 'api-database-detail';
        res = request('GET', BASE_URL + '/api/databases/' + db.id, null, data.submitter, tags);
        const body = parseJSON(res);
        ok = res.status === 200 && body !== null && Array.isArray(body.schemas);
        check(res, { 'api database detail ok': function () { return ok; } }, tags);
    } else {
        tags.name = 'api-table-page';
        const schema = encodeURIComponent(db.schema);
        const table = encodeURIComponent(db.table);
        res = request('GET', BASE_URL + '/api/databases/' + db.id + '/tables/' + schema + '/' + table + '?page=1&page_size=25',
            null, data.submitter, tags);
        const body = parseJSON(res);
        ok = res.status === 200 && body !== null && Array.isArray(body.rows);
        check(res, { 'api table page ok': function () { return ok; } }, tags);
    }
    apiResponseOk.add(ok, tags);
    sleep(API_THINK_SECONDS);
}

function sleepRemaining(stage) {
    const elapsed = Date.now() - exec.scenario.startTime;
    const remaining = (STAGE_SECONDS * 1000) - elapsed;
    if (remaining > 100) {
        sleep(remaining / 1000);
    }
}

function jobIDFrom(res) {
    const body = parseJSON(res);
    return body && body.job_id ? String(body.job_id) : '';
}

function submitJob(data) {
    const stage = STAGE_BY_NAME[exec.scenario.name];
    const iteration = exec.scenario.iterationInTest;
    if (iteration >= stage.jobVus) {
        sleep(1);
        return;
    }
    const position = stage.jobOffset + iteration;
    const slot = JOB_SLOTS[position];
    const migrationID = data.migrations[position];
    const tags = { stage: stage.name, op: 'job-submit', db: slot.db, status: 'submit' };
    if (!migrationID) {
        jobsSuccess.add(false, tags);
        sleepRemaining(stage);
        return;
    }
    const submitRes = request('POST', BASE_URL + '/api/migrations/' + migrationID + '/execute', {}, data.submitter, tags);
    const jobID = jobIDFrom(submitRes);
    const accepted = submitRes.status === 202 && jobID !== '';
    check(submitRes, { 'execute accepted': function () { return accepted; } }, tags);
    jobSubmitAccepted.add(accepted, tags);
    if (!accepted) {
        jobsSuccess.add(false, tags);
        sleepRemaining(stage);
        return;
    }
    const acceptedAt = Date.now();
    let job = null;
    let firstRunningAt = 0;
    const pollTags = Object.assign({}, tags, { op: 'job-poll', status: 'poll', name: 'job-poll' });
    while (Date.now() - acceptedAt < JOB_TIMEOUT_MS) {
        const pollRes = request('GET', BASE_URL + '/api/jobs/' + jobID, null, data.submitter, pollTags);
        const body = parseJSON(pollRes);
        const ok = pollRes.status === 200 && body !== null && typeof body.status === 'string';
        check(pollRes, { 'job poll ok': function () { return ok; } }, pollTags);
        apiResponseOk.add(ok, pollTags);
        if (ok) {
            job = body;
            if (!firstRunningAt && (job.status === 'running' || TERMINAL[job.status])) {
                firstRunningAt = Date.now();
            }
            if (TERMINAL[job.status]) {
                break;
            }
        }
        sleep(POLL_INTERVAL_SECONDS);
    }
    const resultTags = Object.assign({}, tags, { op: 'job-result', status: job && job.status ? job.status : 'timeout' });
    if (job && TERMINAL[job.status]) {
        const success = job.status === 'success';
        jobsCompleted.add(1, resultTags);
        jobsSuccess.add(success, resultTags);
        const queueWait = firstRunningAt && acceptedAt ? firstRunningAt - acceptedAt : 0;
        if (queueWait >= 0) {
            jobQueueWait.add(queueWait, resultTags);
        }
        const serverStart = job.started_at ? Date.parse(job.started_at) : 0;
        const serverFinish = job.finished_at ? Date.parse(job.finished_at) : 0;
        const serverDuration = serverStart && serverFinish && serverFinish >= serverStart ? serverFinish - serverStart : 0;
        const clientDuration = firstRunningAt ? Date.now() - firstRunningAt : Date.now() - acceptedAt;
        jobDuration.add(serverDuration || clientDuration, resultTags);
    } else {
        jobsSuccess.add(false, resultTags);
        jobsTimeout.add(1, resultTags);
    }
    sleepRemaining(stage);
}

function findMetricExact(data, name, wanted) {
    const prefix = name + '{';
    const metrics = data && data.metrics ? data.metrics : {};
    for (const key of Object.keys(metrics)) {
        if (key.indexOf(prefix) !== 0 || key.charAt(key.length - 1) !== '}') {
            continue;
        }
        const parts = key.slice(prefix.length, key.length - 1).split(',');
        const tags = {};
        for (const part of parts) {
            const eq = part.indexOf('=');
            if (eq > 0) {
                tags[part.slice(0, eq)] = part.slice(eq + 1).replace(/^"|"$/g, '');
            }
        }
        const keys = Object.keys(tags);
        const wantedKeys = Object.keys(wanted);
        if (keys.length !== wantedKeys.length) {
            continue;
        }
        let match = true;
        for (const tag of wantedKeys) {
            if (tags[tag] !== wanted[tag]) {
                match = false;
                break;
            }
        }
        if (match) {
            return metrics[key];
        }
    }
    return null;
}

function valuesOf(metric) {
    return metric && metric.values ? metric.values : null;
}

function number(value, digits) {
    if (typeof value !== 'number' || !Number.isFinite(value)) {
        return '-';
    }
    return value.toFixed(digits || 2);
}

function thresholdText(metric, label) {
    if (!metric || !metric.thresholds) {
        return label + ': tidak ada data';
    }
    const parts = [];
    for (const key of Object.keys(metric.thresholds)) {
        const threshold = metric.thresholds[key];
        parts.push(key + '=' + (threshold && threshold.ok ? 'ok' : 'FAIL'));
    }
    return label + ': ' + (parts.length ? parts.join(' ') : '-');
}

export function handleSummary(data) {
    const rows = [];
    for (const stage of STAGES) {
        const read = valuesOf(findMetricExact(data, 'http_req_duration', { op: 'api-read', stage: stage.name }));
        const requests = valuesOf(findMetricExact(data, 'http_reqs', { op: 'api-read', stage: stage.name }));
        const failed = valuesOf(findMetricExact(data, 'http_req_failed', { op: 'api-read', stage: stage.name }));
        const queue = valuesOf(findMetricExact(data, 'job_queue_wait_ms', { stage: stage.name }));
        const duration = valuesOf(findMetricExact(data, 'job_duration_ms', { stage: stage.name }));
        const completed = valuesOf(findMetricExact(data, 'jobs_completed', { stage: stage.name }));
        const success = valuesOf(findMetricExact(data, 'jobs_success', { stage: stage.name }));
        const errorRate = failed && typeof failed.rate === 'number' ? failed.rate : null;
        rows.push({
            stage: stage.name,
            api_vus: stage.apiVus,
            job_vus: stage.jobVus,
            api_p50_ms: read && typeof read.med === 'number' ? read.med : null,
            api_p95_ms: read && typeof read['p(95)'] === 'number' ? read['p(95)'] : null,
            api_requests: requests && typeof requests.count === 'number' ? requests.count : null,
            api_requests_per_second: requests && typeof requests.count === 'number' ? requests.count / STAGE_SECONDS : null,
            api_error_rate: errorRate,
            job_queue_wait_p95_ms: queue && typeof queue['p(95)'] === 'number' ? queue['p(95)'] : null,
            job_duration_avg_ms: duration && typeof duration.avg === 'number' ? duration.avg : null,
            jobs_completed: completed && typeof completed.count === 'number' ? completed.count : null,
            jobs_per_second: completed && typeof completed.count === 'number' ? completed.count / STAGE_SECONDS : null,
            job_success_rate: success && typeof success.rate === 'number' ? success.rate : null,
        });
    }
    const lines = [];
    lines.push('k6 stress ringkas');
    lines.push('base_url=' + BASE_URL + ' stage_seconds=' + STAGE_SECONDS + ' job_timeout_seconds=' + JOB_TIMEOUT_SECONDS);
    lines.push('stage api_vus job_vus api_p50_ms api_p95_ms api_rps api_error_rate queue_p95_ms job_duration_avg_ms job_rps job_success_rate');
    for (const row of rows) {
        lines.push([
            row.stage,
            row.api_vus,
            row.job_vus,
            number(row.api_p50_ms),
            number(row.api_p95_ms),
            row.api_requests_per_second === null ? '-' : number(row.api_requests_per_second),
            row.api_error_rate === null ? '-' : number(row.api_error_rate, 4),
            number(row.job_queue_wait_p95_ms),
            number(row.job_duration_avg_ms),
            row.jobs_per_second === null ? '-' : number(row.jobs_per_second),
            row.job_success_rate === null ? '-' : number(row.job_success_rate, 4),
        ].join(' '));
    }
    const metrics = data && data.metrics ? data.metrics : {};
    lines.push(thresholdText(metrics['http_req_duration'], 'http_req_duration'));
    lines.push(thresholdText(metrics['http_req_duration{op:api-read}'], 'http_req_duration{op:api-read}'));
    lines.push(thresholdText(metrics['http_req_failed'], 'http_req_failed'));
    lines.push(thresholdText(metrics['http_req_failed{op:api-read}'], 'http_req_failed{op:api-read}'));
    lines.push(thresholdText(metrics['job_submit_accepted'], 'job_submit_accepted'));
    lines.push(thresholdText(metrics['job_queue_wait_ms'], 'job_queue_wait_ms'));
    lines.push(thresholdText(metrics['jobs_success'], 'jobs_success'));
    const output = { stdout: lines.join('\n') + '\n' };
    if (SUMMARY_JSON) {
        output[SUMMARY_JSON] = JSON.stringify({ generated_at: new Date().toISOString(), stages: rows, raw: data }, null, 2);
    }
    return output;
}
