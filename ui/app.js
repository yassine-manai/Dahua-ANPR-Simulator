// ── State ─────────────────────────────────────────────────────────────────────
const state = {
  cameras: [],
  activeCameraId: null,
  hbRunning: {},   // cameraId → bool
  autoANPR: null,  // interval handle
  logPollInterval: null,
};

// ── API helpers ───────────────────────────────────────────────────────────────
async function api(method, path, body) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
  };
  if (body !== undefined) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  return res.json();
}

const GET    = (p)    => api('GET', p);
const POST   = (p, b) => api('POST', p, b);
const PUT    = (p, b) => api('PUT', p, b);
const DELETE = (p)    => api('DELETE', p);

// ── UUID generator ────────────────────────────────────────────────────────────
function genUUID() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, c => {
    const r = Math.random() * 16 | 0;
    return (c === 'x' ? r : (r & 0x3 | 0x8)).toString(16);
  });
}

// ── Camera list ───────────────────────────────────────────────────────────────
async function loadCameras() {
  const cameras = await GET('/sim/cameras');
  state.cameras = cameras || [];
  renderCameraList();
  if (state.activeCameraId) {
    const still = state.cameras.find(c => c.id === state.activeCameraId);
    if (still) selectCamera(state.activeCameraId);
    else {
      state.activeCameraId = null;
      setConfigEnabled(false);
      setSendEnabled(false);
    }
  }
}

function renderCameraList() {
  const list = document.getElementById('cameraList');
  if (!state.cameras.length) {
    list.innerHTML = '<div class="empty-state"><span class="icon">📷</span><span>No cameras</span></div>';
    return;
  }
  list.innerHTML = state.cameras.map(cam => `
    <div class="camera-item ${cam.id === state.activeCameraId ? 'active' : ''} ${state.hbRunning[cam.id] ? 'hb-running' : ''}"
         data-id="${cam.id}">
      <span class="cam-dot"></span>
      <div style="flex:1;min-width:0">
        <div class="cam-label">${escHtml(cam.label || cam.id)}</div>
        <div class="cam-url">${escHtml(shortUrl(cam.backend_url))}</div>
      </div>
    </div>
  `).join('');

  list.querySelectorAll('.camera-item').forEach(el => {
    el.addEventListener('click', () => selectCamera(el.dataset.id));
  });
}

function shortUrl(url) {
  if (!url) return '—';
  return url.replace(/^https?:\/\//, '').substring(0, 24);
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// ── Select camera ─────────────────────────────────────────────────────────────
function selectCamera(id) {
  state.activeCameraId = id;
  const cam = state.cameras.find(c => c.id === id);
  if (!cam) return;

  renderCameraList();
  fillConfig(cam);
  setConfigEnabled(true);
  setSendEnabled(true);

  document.getElementById('configCameraLabel').textContent = cam.label || cam.id;
  document.getElementById('sendCameraLabel').textContent   = cam.label || cam.id;
  document.getElementById('logCameraLabel').textContent    = cam.label || cam.id;

  updateHBStatus(id);
  startLogPoll(id);
  refreshAllPreviews(cam);
}

// ── Config form ───────────────────────────────────────────────────────────────
function fillConfig(cam) {
  document.getElementById('cfgLabel').value             = cam.label || '';
  document.getElementById('cfgBackendURL').value        = cam.backend_url || '';
  document.getElementById('cfgDeviceID').value          = cam.device_id || '';
  document.getElementById('cfgDeviceName').value        = cam.device_name || '';
  document.getElementById('cfgDeviceModel').value       = cam.device_model || '';
  document.getElementById('cfgDeviceType').value        = cam.device_type || 'Tollgate';
  document.getElementById('cfgManufacturer').value      = cam.manufacturer || 'Dahua';
  document.getElementById('cfgIPAddress').value         = cam.ip_address || '';
  document.getElementById('cfgHeartbeatInterval').value = cam.heartbeat_interval || 30;
  document.getElementById('cfgPlatePool').value         = (cam.plate_pool || []).join('\n');
}

function readConfig() {
  const plates = document.getElementById('cfgPlatePool').value
    .split('\n').map(s => s.trim()).filter(Boolean);
  return {
    id: state.activeCameraId,
    label:              document.getElementById('cfgLabel').value.trim(),
    backend_url:        document.getElementById('cfgBackendURL').value.trim(),
    device_id:          document.getElementById('cfgDeviceID').value.trim(),
    device_name:        document.getElementById('cfgDeviceName').value.trim(),
    device_model:       document.getElementById('cfgDeviceModel').value.trim(),
    device_type:        document.getElementById('cfgDeviceType').value,
    manufacturer:       document.getElementById('cfgManufacturer').value.trim(),
    ip_address:         document.getElementById('cfgIPAddress').value.trim(),
    heartbeat_interval: parseInt(document.getElementById('cfgHeartbeatInterval').value) || 30,
    plate_pool:         plates,
  };
}

function setConfigEnabled(on) {
  document.getElementById('configPanel').classList.toggle('disabled', !on);
}
function setSendEnabled(on) {
  document.getElementById('tabContent').classList.toggle('disabled', !on);
}

// ── Config save / delete ──────────────────────────────────────────────────────
document.getElementById('btnSaveConfig').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const cam = readConfig();
  await PUT(`/sim/cameras/${state.activeCameraId}`, cam);
  await loadCameras();
  refreshAllPreviews(cam);
  flash('btnSaveConfig', '✓ SAVED');
});

document.getElementById('btnDeleteCamera').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  if (!confirm('Delete this camera?')) return;
  await DELETE(`/sim/cameras/${state.activeCameraId}`);
  state.activeCameraId = null;
  setConfigEnabled(false);
  setSendEnabled(false);
  stopLogPoll();
  document.getElementById('logEntries').innerHTML = '';
  await loadCameras();
});

document.getElementById('btnAddCamera').addEventListener('click', async () => {
  const cam = {
    id:                 'cam_' + Date.now(),
    label:              'New Camera',
    backend_url:        'http://localhost:8080',
    device_id:          genUUID(),
    device_name:        'DH-ITC205',
    device_model:       'DH-ITC205',
    device_type:        'Tollgate',
    manufacturer:       'Dahua',
    ip_address:         '192.168.1.108',
    heartbeat_interval: 30,
    plate_pool:         [],
  };
  await POST('/sim/cameras', cam);
  await loadCameras();
  selectCamera(cam.id);
});

document.getElementById('btnGenUUID').addEventListener('click', () => {
  document.getElementById('cfgDeviceID').value = genUUID();
});

// ── Heartbeat ─────────────────────────────────────────────────────────────────
document.getElementById('btnHBStart').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  await POST(`/sim/cameras/${state.activeCameraId}/heartbeat/start`);
  state.hbRunning[state.activeCameraId] = true;
  updateHBStatus(state.activeCameraId);
  renderCameraList();
});

document.getElementById('btnHBStop').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  await POST(`/sim/cameras/${state.activeCameraId}/heartbeat/stop`);
  state.hbRunning[state.activeCameraId] = false;
  updateHBStatus(state.activeCameraId);
  renderCameraList();
});

function updateHBStatus(id) {
  const el = document.getElementById('hbStatus');
  const running = state.hbRunning[id];
  el.textContent = running ? '● RUNNING' : '● STOPPED';
  el.className = 'hb-status' + (running ? ' running' : '');
}

// ── Tabs ──────────────────────────────────────────────────────────────────────
document.querySelectorAll('.tab').forEach(tab => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
    tab.classList.add('active');
    document.getElementById('tab-' + tab.dataset.tab).classList.add('active');
    if (state.activeCameraId) {
      const cam = state.cameras.find(c => c.id === state.activeCameraId);
      if (cam) refreshAllPreviews(cam);
    }
  });
});

// ── Send: DeviceInfo ──────────────────────────────────────────────────────────
document.getElementById('btnSendDevInfo').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/deviceinfo`, { overrides: {} });
  setStatus(res.status);
});

// ── Send: Heartbeat ───────────────────────────────────────────────────────────
document.getElementById('btnSendKA').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/keepalive`, { overrides: {} });
  setStatus(res.status);
});

// ── Send: ANPR ────────────────────────────────────────────────────────────────
document.getElementById('btnSendANPR').addEventListener('click', () => sendANPR());

async function sendANPR() {
  if (!state.activeCameraId) return;
  const overrides = buildANPROverrides();
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/anpr`, { overrides });
  setStatus(res.status);
}

function buildANPROverrides() {
  const plate  = document.getElementById('anprPlate').value.trim();
  const color  = document.getElementById('anprPlateColor').value;
  const type   = document.getElementById('anprPlateType').value;
  const dir    = document.getElementById('anprDirection').value;
  const vColor = document.getElementById('anprVehicleColor').value;
  const vType  = document.getElementById('anprVehicleType').value;
  const lane   = parseInt(document.getElementById('anprLane').value) || 1;
  const strobe = document.getElementById('anprStrobe').value === 'true';

  return {
    Picture: {
      Plate: {
        IsExist: !!plate,
        PlateNumber: plate,
        PlateColor: color,
        PlateType: type,
        Confidence: 90,
        BoundingBox: [100, 200, 300, 260],
        Channel: 0,
      },
      Vehicle: {
        VehicleColor: vColor,
        VehicleType: vType,
      },
    },
    SnapInfo: {
      TriggerSource: 'Video',
      SnapTime: nowStr(),
      AccurateTime: nowMsStr(),
      TimeZone: 2,
      DSTTune: 0,
      LanNo: lane,
      Direction: dir,
      OpenStrobe: strobe,
      AllowUser: false,
      BlockUser: false,
      DefenceCode: 'SIM001',
      DeviceID: activeCam()?.device_id || '',
    },
  };
}

// Auto ANPR
let autoANPRRunning = false;
let autoANPRHandle  = null;

document.getElementById('btnAutoANPR').addEventListener('click', () => {
  const btn = document.getElementById('btnAutoANPR');
  if (autoANPRRunning) {
    clearInterval(autoANPRHandle);
    autoANPRRunning = false;
    btn.textContent = '⟳ AUTO';
    btn.classList.remove('running');
  } else {
    const secs = parseInt(document.getElementById('autoANPRInterval').value) || 10;
    autoANPRRunning = true;
    btn.textContent = '■ STOP AUTO';
    btn.classList.add('running');
    sendANPR();
    autoANPRHandle = setInterval(sendANPR, secs * 1000);
  }
});

// ── Send: Parking ─────────────────────────────────────────────────────────────
document.getElementById('btnSendParking').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const overrides = buildParkingOverrides();
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/parking`, { overrides });
  setStatus(res.status);
});

function buildParkingOverrides() {
  const plate     = document.getElementById('parkPlate').value.trim();
  const stall     = document.getElementById('parkStall').value.trim();
  const status    = parseInt(document.getElementById('parkStatus').value);
  const direction = document.getElementById('parkDirection').value;
  const inRecord  = document.getElementById('parkInRecordId').value.trim();

  const info = {
    SnapTime:        nowStr(),
    TimeZone:        2,
    DSTTune:         0,
    Channel:         0,
    ParkingStallsNo: stall || 'A01',
    Direction:       direction,
    ParkingStatus:   status,
    AllowUser:       false,
    BlockUser:       false,
  };
  if (inRecord) info.inRecordId = inRecord;

  return {
    Picture: {
      Plate: {
        IsExist: !!plate,
        PlateNumber: plate,
        PlateColor: 'White',
        PlateType: 'Normal',
        Confidence: 85,
        BoundingBox: [100, 200, 300, 260],
      },
    },
    ParkingInfo: info,
    DeviceID: activeCam()?.device_id || '',
  };
}

// ── Send: Alarm ───────────────────────────────────────────────────────────────
document.getElementById('btnSendAlarm').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const overrides = buildAlarmOverrides();
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/alarm`, { overrides });
  setStatus(res.status);
});

function buildAlarmOverrides() {
  const type  = parseInt(document.getElementById('alarmType').value);
  const state_ = document.getElementById('alarmState').value;
  const stall = document.getElementById('alarmStall').value.trim();
  const plate = document.getElementById('alarmPlate').value.trim();

  const info = {
    Time:     nowStr(),
    Type:     type,
    State:    state_,
    TimeZone: 2,
    DSTTune:  0,
  };
  if (stall) info.ParkingStallsNo = stall;
  if (plate) info.PlateInfoList = [{ PlateNumber: plate, TimeZone: 2, DSTTune: 0 }];

  return {
    AlarmInfo: info,
    DeviceID: activeCam()?.device_id || '',
  };
}

// ── Payload previews ──────────────────────────────────────────────────────────
function refreshAllPreviews(cam) {
  // DevInfo preview
  document.getElementById('preview-deviceinfo').textContent = JSON.stringify({
    DeviceName:   cam.device_name,
    DeviceModel:  cam.device_model,
    DeviceType:   cam.device_type,
    Manufacturer: cam.manufacturer,
    IPAddress:    cam.ip_address,
    DeviceID:     cam.device_id,
  }, null, 2);

  // Heartbeat preview
  document.getElementById('preview-keepalive').textContent = JSON.stringify({
    Active:   'keepAlive',
    DeviceID: cam.device_id,
  }, null, 2);

  // ANPR preview
  document.getElementById('preview-anpr').textContent = JSON.stringify(buildANPROverrides(), null, 2);

  // Parking preview
  document.getElementById('preview-parking').textContent = JSON.stringify(buildParkingOverrides(), null, 2);

  // Alarm preview
  document.getElementById('preview-alarm').textContent = JSON.stringify(buildAlarmOverrides(), null, 2);
}

// Live preview update on field change
['anprPlate','anprPlateColor','anprPlateType','anprDirection','anprVehicleColor','anprVehicleType','anprLane','anprStrobe'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', () => {
    document.getElementById('preview-anpr').textContent = JSON.stringify(buildANPROverrides(), null, 2);
  });
});
['parkPlate','parkStall','parkStatus','parkDirection','parkInRecordId'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', () => {
    document.getElementById('preview-parking').textContent = JSON.stringify(buildParkingOverrides(), null, 2);
  });
});
['alarmType','alarmState','alarmStall','alarmPlate'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', () => {
    document.getElementById('preview-alarm').textContent = JSON.stringify(buildAlarmOverrides(), null, 2);
  });
});

// ── Log ───────────────────────────────────────────────────────────────────────
function startLogPoll(id) {
  stopLogPoll();
  pollLog(id);
  state.logPollInterval = setInterval(() => pollLog(id), 2000);
}

function stopLogPoll() {
  if (state.logPollInterval) clearInterval(state.logPollInterval);
  state.logPollInterval = null;
}

let lastLogCount = 0;

async function pollLog(id) {
  const entries = await GET(`/sim/cameras/${id}/log`);
  if (!Array.isArray(entries)) return;
  if (entries.length === lastLogCount) return;
  lastLogCount = entries.length;
  renderLog(entries);
}

function renderLog(entries) {
  const container = document.getElementById('logEntries');
  if (!entries.length) {
    container.innerHTML = '<div style="padding:12px;color:var(--text3);font-family:var(--font-mono);font-size:11px;">No requests yet.</div>';
    return;
  }
  container.innerHTML = entries.map((e, i) => {
    const statusClass = !e.status_code ? 'err' : e.status_code < 300 ? 'ok' : e.status_code < 500 ? 'warn' : 'err';
    const statusText  = e.status_code || (e.error ? 'ERR' : '—');
    const bodyPreview = e.error ? e.error : (e.res_body || '').substring(0, 120);

    return `
      <div class="log-entry" data-i="${i}" onclick="toggleLogEntry(this)">
        <span class="log-time">${escHtml(e.time)}</span>
        <span class="log-method">POST</span>
        <span class="log-endpoint">${escHtml(e.endpoint)}</span>
        <span class="log-status ${statusClass}">${statusText}</span>
        <span class="log-body">${escHtml(bodyPreview)}</span>
        <div class="log-entry-details">
          <div class="log-req-label">REQUEST</div>
          <pre style="color:var(--text2);font-size:10px;white-space:pre-wrap;word-break:break-all;margin-bottom:6px">${escHtml(prettyJSON(e.req_body))}</pre>
          <div class="log-res-label">RESPONSE</div>
          <pre style="color:var(--text2);font-size:10px;white-space:pre-wrap;word-break:break-all">${escHtml(e.error ? '⚠ ' + e.error : prettyJSON(e.res_body))}</pre>
        </div>
      </div>
    `;
  }).join('');
}

function toggleLogEntry(el) {
  el.classList.toggle('expanded');
}

document.getElementById('btnClearLog').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  await DELETE(`/sim/cameras/${state.activeCameraId}/log`);
  lastLogCount = 0;
  document.getElementById('logEntries').innerHTML = '';
});

// ── Global status indicator ───────────────────────────────────────────────────
function setStatus(statusCode) {
  const dot  = document.getElementById('globalStatus');
  const text = document.getElementById('globalStatusText');
  if (!statusCode) {
    dot.className = 'status-dot error';
    text.textContent = 'ERROR';
  } else if (statusCode < 300) {
    dot.className = 'status-dot active';
    text.textContent = statusCode + ' OK';
  } else {
    dot.className = 'status-dot error';
    text.textContent = statusCode + ' ERR';
  }
  setTimeout(() => {
    dot.className = 'status-dot';
    text.textContent = 'IDLE';
  }, 3000);
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function activeCam() {
  return state.cameras.find(c => c.id === state.activeCameraId) || null;
}

function nowStr() {
  return new Date().toISOString().replace('T', ' ').substring(0, 19);
}

function nowMsStr() {
  return new Date().toISOString().replace('T', ' ').substring(0, 23);
}

function prettyJSON(s) {
  if (!s) return '';
  try { return JSON.stringify(JSON.parse(s), null, 2); }
  catch { return s; }
}

function flash(btnId, msg) {
  const btn = document.getElementById(btnId);
  const orig = btn.textContent;
  btn.textContent = msg;
  setTimeout(() => { btn.textContent = orig; }, 1500);
}

// ── Init ──────────────────────────────────────────────────────────────────────
loadCameras();

// ── Theme toggle ──────────────────────────────────────────────────────────────
(function () {
  const btn = document.getElementById('btnTheme');
  const root = document.documentElement;

  // Restore saved preference
  const saved = localStorage.getItem('sim-theme');
  if (saved === 'dark') {
    root.setAttribute('data-theme', 'dark');
    btn.textContent = '🌙';
  } else {
    btn.textContent = '☀';
  }

  btn.addEventListener('click', () => {
    const isDark = root.getAttribute('data-theme') === 'dark';
    if (isDark) {
      root.removeAttribute('data-theme');
      btn.textContent = '☀';
      localStorage.setItem('sim-theme', 'light');
    } else {
      root.setAttribute('data-theme', 'dark');
      btn.textContent = '🌙';
      localStorage.setItem('sim-theme', 'dark');
    }
  });
})();