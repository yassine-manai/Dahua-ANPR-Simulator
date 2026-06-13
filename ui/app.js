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
  document.getElementById('cfgIPv6Address').value       = cam.ipv6_address || '';
  document.getElementById('cfgMACAddress').value        = cam.mac_address || '';
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
    ipv6_address:       document.getElementById('cfgIPv6Address').value.trim(),
    mac_address:        document.getElementById('cfgMACAddress').value.trim(),
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
  const plate       = document.getElementById('anprPlate').value.trim();
  const color       = document.getElementById('anprPlateColor').value;
  const type        = document.getElementById('anprPlateType').value;
  const region      = document.getElementById('anprRegion').value.trim();
  const dir         = document.getElementById('anprDirection').value;
  const trigger     = document.getElementById('anprTrigger').value;
  const vColor      = document.getElementById('anprVehicleColor').value;
  const vType       = document.getElementById('anprVehicleType').value;
  const vSign       = document.getElementById('anprVehicleSign').value.trim();
  const speed       = parseInt(document.getElementById('anprSpeed').value) || 0;
  const lane        = parseInt(document.getElementById('anprLane').value) || 1;
  const channel     = parseInt(document.getElementById('anprChannel').value) || 0;
  const strobe      = document.getElementById('anprStrobe').value === 'true';
  const peopleNum   = parseInt(document.getElementById('anprPeopleNum').value) || 0;
  const defCode     = document.getElementById('anprDefenceCode').value.trim() || 'SIM001';
  const snapAddr    = document.getElementById('anprSnapAddress').value.trim();
  const cam         = activeCam();

  const plateObj = {
    IsExist:     !!plate,
    PlateNumber: plate,
    PlateColor:  color,
    PlateType:   type,
    Confidence:  90,
    BoundingBox: [100, 200, 300, 260],
    Channel:     channel,
  };
  if (region) plateObj.Region = region;

  const vehicleObj = {
    VehicleColor: vColor,
    VehicleType:  vType,
  };
  if (vSign)  vehicleObj.VehicleSign = vSign;
  if (speed)  vehicleObj.Speed = speed;

  const snapInfo = {
    Source: trigger,
    SnapTime:      nowStr(),
    AccurateTime:  nowMsStr(),
    TimeZone:      2,
    DSTTune:       0,
    LanNo:         lane,
    Direction:     dir,
    OpenStrobe:    strobe,
    AllowUser:     false,
    BlockUser:     false,
    DefenceCode:   defCode,
    DeviceID:      cam?.device_id || '',
  };
  if (snapAddr)   snapInfo.SnapAddress    = snapAddr;
  if (peopleNum)  snapInfo.InCarPeopleNum = peopleNum;

  return {
    Picture: {
      Plate:    plateObj,
      Vehicle:  vehicleObj,
      SnapInfo: snapInfo,
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
  const plate      = document.getElementById('parkPlate').value.trim();
  const plateColor = document.getElementById('parkPlateColor').value;
  const plateType  = document.getElementById('parkPlateType').value;
  const region     = document.getElementById('parkRegion').value.trim();
  const stall      = document.getElementById('parkStall').value.trim();
  const channel    = parseInt(document.getElementById('parkChannel').value) || 0;
  const status     = parseInt(document.getElementById('parkStatus').value);
  const direction  = document.getElementById('parkDirection').value;
  const vColor     = document.getElementById('parkVehicleColor').value;
  const vType      = document.getElementById('parkVehicleType').value;
  const vSign      = document.getElementById('parkVehicleSign').value.trim();
  const inRecord   = document.getElementById('parkInRecordId').value.trim();
  const cam        = activeCam();

  const plateObj = {
    IsExist:     !!plate,
    PlateNumber: plate,
    PlateColor:  plateColor,
    PlateType:   plateType,
    Confidence:  85,
    BoundingBox: [100, 200, 300, 260],
  };
  if (region) plateObj.Region = region;

  const vehicleObj = { VehicleColor: vColor, VehicleType: vType };
  if (vSign) vehicleObj.VehicleSign = vSign;

  const info = {
    SnapTime:        nowStr(),
    TimeZone:        2,
    DSTTune:         0,
    Channel:         channel,
    ParkingStallsNo: stall || '',
    Direction:       direction,
    ParkingStatus:   status,
    AllowUser:       false,
    BlockUser:       false,
    DeviceID:        cam?.device_id || '',
  };
  if (inRecord) info.inRecordId = inRecord;

  const payload = {
    Picture: {
      Plate:       plateObj,
      Vehicle:     vehicleObj,
      ParkingInfo: info,
    },
  };

  return payload;
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
  const devInfoPayload = {
    DeviceName:   cam.device_name,
    DeviceModel:  cam.device_model,
    DeviceType:   cam.device_type,
    Manufacturer: cam.manufacturer,
    DeviceID:     cam.device_id,
  };
  if (cam.ip_address)   devInfoPayload.IPAddress   = cam.ip_address;
  if (cam.ipv6_address) devInfoPayload.IPv6Address = cam.ipv6_address;
  if (cam.mac_address)  devInfoPayload.MACAddress  = cam.mac_address;
  document.getElementById('preview-deviceinfo').textContent = JSON.stringify(devInfoPayload, null, 2);

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
['anprPlate','anprPlateColor','anprPlateType','anprRegion','anprDirection','anprTrigger','anprVehicleColor','anprVehicleType','anprVehicleSign','anprSpeed','anprLane','anprChannel','anprStrobe','anprPeopleNum','anprDefenceCode','anprSnapAddress'].forEach(id => {
  document.getElementById(id)?.addEventListener('input', () => {
    document.getElementById('preview-anpr').textContent = JSON.stringify(buildANPROverrides(), null, 2);
  });
});
['parkPlate','parkPlateColor','parkPlateType','parkRegion','parkStall','parkChannel','parkStatus','parkDirection','parkVehicleColor','parkVehicleType','parkVehicleSign','parkInRecordId'].forEach(id => {
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


// ── Multi-Spot ────────────────────────────────────────────────────────────────
// Channels/spots are stored inside the camera config (cam.channels).
// Every structural change (add/remove channel or spot, edit label/id/plate)
// is saved immediately via PUT /sim/cameras/:id so sim_config.json stays
// in sync without a separate Save button.

const MAX_SPOTS = 4;

// Called whenever the active camera changes or the multispot tab is opened.
function renderMultiSpot() {
  const cam = activeCam();
  if (!cam) return;

  const channels = cam.channels || [];
  const container = document.getElementById('msChannels');
  container.innerHTML = '';

  channels.forEach((ch, chIdx) => {
    container.appendChild(buildChannelEl(cam, ch, chIdx));
  });
}

// ── Build a channel block ─────────────────────────────────────────────────────
function buildChannelEl(cam, ch, chIdx) {
  const el = document.createElement('div');
  el.className = 'ms-channel' + ((ch.spots || []).length >= MAX_SPOTS ? ' ms-full' : '');
  el.dataset.chIdx = chIdx;

  // Header
  const hdr = document.createElement('div');
  hdr.className = 'ms-channel-header';

  const badge = document.createElement('span');
  badge.className = 'ms-channel-badge';
  badge.textContent = 'CH' + ch.channel_no;

  const labelInput = document.createElement('input');
  labelInput.className = 'ms-channel-label';
  labelInput.type = 'text';
  labelInput.value = ch.label || ('Channel ' + ch.channel_no);
  labelInput.placeholder = 'Channel label';
  labelInput.addEventListener('change', () => {
    msPatch(cam, chIdx, null, { label: labelInput.value });
  });

  const removeBtn = document.createElement('button');
  removeBtn.className = 'ms-channel-remove';
  removeBtn.textContent = '✕';
  removeBtn.title = 'Remove channel';
  removeBtn.addEventListener('click', () => msRemoveChannel(cam, chIdx));

  hdr.appendChild(badge);
  hdr.appendChild(labelInput);
  hdr.appendChild(removeBtn);

  // Spots row
  const row = document.createElement('div');
  row.className = 'ms-spots-row';

  (ch.spots || []).forEach((spot, sIdx) => {
    row.appendChild(buildSpotCard(cam, ch, chIdx, spot, sIdx));
  });

  // Add-spot tile
  const addTile = document.createElement('button');
  addTile.className = 'ms-add-spot';
  addTile.textContent = '+';
  addTile.title = 'Add spot';
  addTile.addEventListener('click', () => msAddSpot(cam, chIdx));
  row.appendChild(addTile);

  el.appendChild(hdr);
  el.appendChild(row);
  return el;
}

// ── Build a spot card ─────────────────────────────────────────────────────────
function buildSpotCard(cam, ch, chIdx, spot, sIdx) {
  const card = document.createElement('div');
  card.className = 'ms-spot-card';

  // Remove button
  const removeBtn = document.createElement('button');
  removeBtn.className = 'ms-spot-remove';
  removeBtn.textContent = '✕';
  removeBtn.title = 'Remove spot';
  removeBtn.addEventListener('click', () => msRemoveSpot(cam, chIdx, sIdx));
  card.appendChild(removeBtn);

  // Spot ID field
  const idWrap = document.createElement('div');
  idWrap.className = 'ms-spot-field';
  const idLabel = document.createElement('label');
  idLabel.textContent = 'Spot ID';
  const idInput = document.createElement('input');
  idInput.className = 'ms-spot-input';
  idInput.type = 'text';
  idInput.value = spot.id || '';
  idInput.placeholder = 'CH' + ch.channel_no + '-S' + (sIdx + 1);
  idInput.addEventListener('change', () => {
    msPatch(cam, chIdx, sIdx, { id: idInput.value });
  });
  idWrap.appendChild(idLabel);
  idWrap.appendChild(idInput);
  card.appendChild(idWrap);

  // Image preview (shown when an image is selected)
  const previewEl = document.createElement('div');
  previewEl.className = 'ms-spot-preview';
  const previewImg = document.createElement('img');
  previewImg.className = 'ms-spot-preview-img';
  previewEl.appendChild(previewImg);
  card.appendChild(previewEl);

  // Image picker
  const imgWrap = document.createElement('div');
  imgWrap.className = 'ms-spot-field';
  const imgLabel = document.createElement('label');
  imgLabel.textContent = 'Image';

  // Hidden select — ImgPicker wraps this
  const imgSel = document.createElement('select');
  imgSel.innerHTML = '<option value="">— none —</option>';
  imgLib.list.forEach(img => {
    const opt = document.createElement('option');
    opt.value = img.id;
    opt.textContent = img.name;
    imgSel.appendChild(opt);
  });
  if (spot.image_id) imgSel.value = spot.image_id;

  imgWrap.appendChild(imgLabel);
  imgWrap.appendChild(imgSel);
  card.appendChild(imgWrap);

  // Mount ImgPicker on the select — it injects its own UI around the hidden select
  const spotPicker = new ImgPicker(imgSel, (imgId) => {
    msPatch(cam, chIdx, sIdx, { image_id: imgId });
    msShowPreview(previewEl, previewImg, imgId);
  });

  // Show preview on load if image_id was already set
  if (spot.image_id) msShowPreview(previewEl, previewImg, spot.image_id);

  // Plate field
  const plateWrap = document.createElement('div');
  plateWrap.className = 'ms-spot-field';
  const plateLabel = document.createElement('label');
  plateLabel.textContent = 'Plate';
  const plateInput = document.createElement('input');
  plateInput.className = 'ms-spot-input';
  plateInput.type = 'text';
  plateInput.value = spot.plate || '';
  plateInput.placeholder = 'TN0000AA';
  plateInput.addEventListener('change', () => {
    msPatch(cam, chIdx, sIdx, { plate: plateInput.value });
  });
  plateWrap.appendChild(plateLabel);
  plateWrap.appendChild(plateInput);
  card.appendChild(plateWrap);

  // Send button
  const sendBtn = document.createElement('button');
  sendBtn.className = 'ms-spot-send';
  sendBtn.textContent = '▶ SEND';
  sendBtn.addEventListener('click', () => {
    // Always read fresh state — avoids stale closure values
    const freshCam   = activeCam() || cam;
    const freshCh    = (freshCam.channels || [])[chIdx] || ch;
    const freshSpot  = (freshCh.spots || [])[sIdx] || {};
    const spotId     = idInput.value.trim() || idInput.placeholder;
    const plate      = plateInput.value.trim() || freshSpot.plate || '';
    const imgId      = imgSel.value || freshSpot.image_id || '';
    msSendEvent(freshCam, freshCh, spotId, plate, imgId, card, statusEl);
  });
  card.appendChild(sendBtn);

  // Status line
  const statusEl = document.createElement('div');
  statusEl.className = 'ms-spot-status';
  card.appendChild(statusEl);

  return card;
}

// Show/hide the preview thumbnail on a spot card
function msShowPreview(previewEl, previewImg, imgId) {
  if (!imgId) {
    previewEl.style.display = 'none';
    previewImg.src = '';
    return;
  }
  previewEl.style.display = 'block';
  // Use cached data if available
  if (imgLib.data[imgId]) {
    previewImg.src = 'data:image/*;base64,' + imgLib.data[imgId];
    return;
  }
  // Fetch and cache
  GET('/sim/images/' + imgId).then(full => {
    if (full && full.data) {
      imgLib.data[imgId] = full.data;
      previewImg.src = 'data:image/*;base64,' + full.data;
    }
  });
}

// ── Send parking detection event ──────────────────────────────────────────────
async function msSendEvent(cam, ch, spotId, plate, imageId, cardEl, statusEl) {
  cardEl.classList.remove('sent-ok', 'sent-err');
  cardEl.classList.add('sending');
  statusEl.className = 'ms-spot-status';
  statusEl.textContent = '…';

  const now = nowStr();
  const overrides = {
    Picture: {
      Plate: {
        IsExist:     !!plate,
        PlateNumber: plate || '',
        PlateColor:  'White',
        PlateType:   '',
        Confidence:  85,
        BoundingBox: [100, 200, 300, 260],
      },
      ParkingInfo: {
        SnapTime:        now,
        TimeZone:        2,
        DSTTune:         0,
        Channel:         ch.channel_no,
        ParkingStallsNo: spotId,
        Direction:       'Obverse',
        ParkingStatus:   4,
        AllowUser:       false,
        BlockUser:       false,
        DeviceID:        cam.device_id,
      },
    },
  };

  // Attach this spot's own image if selected (used for both scene and plate cutout)
  if (imageId) {
    const meta = imgLib.list.find(i => i.id === imageId);
    const data = await imgResolve(imageId);
    if (data && meta) {
      overrides.Picture.NormalPic = { PicName: meta.name, Content: data };
      overrides.Picture.CutoutPic = { PicName: meta.name.replace(/(\.[^.]+)$/, '-plate$1'), Content: data };
    }
  }

  try {
    const res = await POST(`/sim/cameras/${cam.id}/send/parking`, { overrides });
    cardEl.classList.remove('sending');
    if (res.status && res.status < 300) {
      cardEl.classList.add('sent-ok');
      statusEl.className = 'ms-spot-status ok';
      statusEl.textContent = res.status + ' OK';
      setStatus(res.status);
    } else {
      cardEl.classList.add('sent-err');
      statusEl.className = 'ms-spot-status err';
      statusEl.textContent = res.status ? (res.status + ' ERR') : 'ERR';
      setStatus(res.status || 0);
    }
  } catch (e) {
    cardEl.classList.remove('sending');
    cardEl.classList.add('sent-err');
    statusEl.className = 'ms-spot-status err';
    statusEl.textContent = 'NET ERR';
    setStatus(0);
  }

  setTimeout(() => {
    cardEl.classList.remove('sent-ok', 'sent-err');
    statusEl.className = 'ms-spot-status';
    statusEl.textContent = '';
  }, 3000);
}

// ── Structural mutations — all auto-save ──────────────────────────────────────

function msAddChannel(cam) {
  const channels = cam.channels || [];
  const nextNo = channels.length > 0
    ? Math.max(...channels.map(c => c.channel_no)) + 1
    : 0;
  channels.push({
    channel_no: nextNo,
    label: 'Channel ' + nextNo,
    spots: [],
  });
  cam.channels = channels;
  msSave(cam);
}

function msRemoveChannel(cam, chIdx) {
  if (!confirm('Remove this channel and all its spots?')) return;
  cam.channels.splice(chIdx, 1);
  msSave(cam);
}

function msAddSpot(cam, chIdx) {
  const ch = cam.channels[chIdx];
  if ((ch.spots || []).length >= MAX_SPOTS) return;
  const sIdx = ch.spots.length;
  const autoId = 'CH' + ch.channel_no + '-S' + (sIdx + 1);
  const plate = msRandomPlate(cam);
  ch.spots.push({ id: autoId, plate });
  msSave(cam);
}

function msRemoveSpot(cam, chIdx, sIdx) {
  cam.channels[chIdx].spots.splice(sIdx, 1);
  msSave(cam);
}

// Patch a single field on a channel or spot and save.
// chIdx: channel index, sIdx: spot index or null for channel-level patch.
function msPatch(cam, chIdx, sIdx, fields) {
  if (sIdx === null) {
    Object.assign(cam.channels[chIdx], fields);
  } else {
    Object.assign(cam.channels[chIdx].spots[sIdx], fields);
  }
  msSaveQuiet(cam); // no re-render — inputs keep focus
}

// Save + re-render
async function msSave(cam) {
  await PUT(`/sim/cameras/${cam.id}`, cam);
  // Refresh local state
  const idx = state.cameras.findIndex(c => c.id === cam.id);
  if (idx !== -1) state.cameras[idx] = cam;
  renderMultiSpot();
  renderCameraList();
}

// Save without re-render (used during inline edits)
async function msSaveQuiet(cam) {
  await PUT(`/sim/cameras/${cam.id}`, cam);
  const idx = state.cameras.findIndex(c => c.id === cam.id);
  if (idx !== -1) state.cameras[idx] = cam;
}

function msRandomPlate(cam) {
  const pool = (cam.plate_pool || []).filter(p => p.trim());
  if (!pool.length) return '';
  return pool[Math.floor(Math.random() * pool.length)];
}

// ── Wire add-channel button ───────────────────────────────────────────────────
document.getElementById('btnAddChannel').addEventListener('click', () => {
  const cam = activeCam();
  if (!cam) return;
  msAddChannel(cam);
});

// ── Re-render multispot when its tab is activated ─────────────────────────────
// Patch the existing tab click handler to call renderMultiSpot for this tab.
document.querySelectorAll('.tab').forEach(tab => {
  if (tab.dataset.tab === 'multispot') {
    tab.addEventListener('click', () => {
      if (state.activeCameraId) renderMultiSpot();
    });
  }
});

// Also render when camera is selected and multispot tab is already active.
const _origSelectCamera = selectCamera;
// Wrap selectCamera to also refresh multispot if that tab is visible
window._msSelectHook = function() {
  const activeTab = document.querySelector('.tab.active');
  if (activeTab && activeTab.dataset.tab === 'multispot') {
    renderMultiSpot();
  }
};

// ── Image Library ─────────────────────────────────────────────────────────────
// Global image state — loaded once, refreshed after add/delete.
const imgLib = {
  list: [],   // [{ id, name }] — meta only, no data
  data: {},   // id → base64 string, lazy-loaded on first use
};

async function imgLoad() {
  const meta = await GET('/sim/images');
  imgLib.list = Array.isArray(meta) ? meta : [];
  imgRenderThumbs();
  imgPopulateSelects();
  const countEl = document.getElementById('imgCount');
  if (countEl) countEl.textContent = `(${imgLib.list.length}/20)`;
  // Re-render multispot so spot card pickers reflect the updated library
  const activeTab = document.querySelector('.tab.active');
  if (activeTab && activeTab.dataset.tab === 'multispot') renderMultiSpot();
}

// ── Thumbnails in config panel ────────────────────────────────────────────────
function imgRenderThumbs() {
  const container = document.getElementById('imgThumbs');
  if (!container) return;
  container.innerHTML = '';
  imgLib.list.forEach(img => {
    const card = document.createElement('div');
    card.className = 'img-thumb-card';

    // Use cached data-URI if we have it, otherwise fetch
    const src = imgLib.data[img.id]
      ? 'data:image/*;base64,' + imgLib.data[img.id]
      : '';

    const thumb = document.createElement('img');
    thumb.alt = img.name;
    if (src) {
      thumb.src = src;
    } else {
      // Lazy-load thumbnail
      GET('/sim/images/' + img.id).then(full => {
        if (full && full.data) {
          imgLib.data[img.id] = full.data;
          thumb.src = 'data:image/*;base64,' + full.data;
        }
      });
    }

    const nameEl = document.createElement('div');
    nameEl.className = 'img-thumb-name';
    nameEl.textContent = img.name;
    nameEl.title = img.name;

    const removeBtn = document.createElement('button');
    removeBtn.className = 'img-thumb-remove';
    removeBtn.textContent = '✕';
    removeBtn.title = 'Remove';
    removeBtn.addEventListener('click', async (e) => {
      e.stopPropagation();
      await DELETE('/sim/images/' + img.id);
      delete imgLib.data[img.id];
      await imgLoad();
    });

    card.appendChild(thumb);
    card.appendChild(nameEl);
    card.appendChild(removeBtn);
    container.appendChild(card);
  });
}

// ── ImgPicker — custom dropdown with thumbnails ───────────────────────────────
// Wraps a hidden <select> so all existing .value reads keep working.
// Also updates the paired tab-img-preview div when selection changes.
const TAB_PICKER_PREVIEWS = {
  anprImgNormal:  'prevAnprNormal',
  anprImgVehicle: 'prevAnprVehicle',
  anprImgCutout:  'prevAnprCutout',
  parkImgNormal:  'prevParkNormal',
  parkImgVehicle: 'prevParkVehicle',
};

class ImgPicker {
  // selectEl  — the hidden <select> to mirror
  // onChange  — optional extra callback(imgId)
  constructor(selectEl, onChange) {
    this.sel      = selectEl;
    this.onChange = onChange || null;
    this._build();
    // Close on outside click
    this._onDocClick = (e) => {
      if (!this.root.contains(e.target)) this._close();
    };
    document.addEventListener('click', this._onDocClick);
  }

  _build() {
    // Wrap the hidden select in a relative container
    const wrapper = document.createElement('div');
    wrapper.className = 'imgpicker-wrap';
    this.sel.parentNode.insertBefore(wrapper, this.sel);
    wrapper.appendChild(this.sel);
    this.sel.style.display = 'none';

    // Trigger button
    this.trigger = document.createElement('button');
    this.trigger.type = 'button';
    this.trigger.className = 'imgpicker-trigger';
    this.trigger.addEventListener('click', (e) => {
      e.stopPropagation();
      this._toggle();
    });
    wrapper.appendChild(this.trigger);

    // Dropdown panel
    this.panel = document.createElement('div');
    this.panel.className = 'imgpicker-panel';
    wrapper.appendChild(this.panel);

    this.root = wrapper;
    this._renderTrigger();
    this._renderOptions();
  }

  _renderTrigger() {
    const imgId = this.sel.value;
    const meta  = imgId ? imgLib.list.find(i => i.id === imgId) : null;
    this.trigger.innerHTML = '';

    if (meta && imgLib.data[imgId]) {
      const thumb = document.createElement('img');
      thumb.src = 'data:image/*;base64,' + imgLib.data[imgId];
      thumb.className = 'imgpicker-trigger-thumb';
      this.trigger.appendChild(thumb);
    } else if (meta) {
      // Fetch then re-render
      imgResolve(imgId).then(() => this._renderTrigger());
      const ph = document.createElement('span');
      ph.className = 'imgpicker-trigger-ph';
      ph.textContent = '🖼';
      this.trigger.appendChild(ph);
    } else {
      const ph = document.createElement('span');
      ph.className = 'imgpicker-trigger-ph';
      ph.textContent = '— none —';
      this.trigger.appendChild(ph);
    }

    const name = document.createElement('span');
    name.className = 'imgpicker-trigger-name';
    name.textContent = meta ? meta.name : '';
    this.trigger.appendChild(name);

    const arrow = document.createElement('span');
    arrow.className = 'imgpicker-arrow';
    arrow.textContent = '▾';
    this.trigger.appendChild(arrow);
  }

  _renderOptions() {
    this.panel.innerHTML = '';

    // None option
    const noneRow = this._makeRow('', null, '— none —');
    noneRow.addEventListener('click', () => this._select(''));
    this.panel.appendChild(noneRow);

    imgLib.list.forEach(img => {
      const row = this._makeRow(img.id, imgLib.data[img.id] || null, img.name);
      // Lazy-load thumb for this row if not cached
      if (!imgLib.data[img.id]) {
        imgResolve(img.id).then(data => {
          if (data) {
            const thumbEl = row.querySelector('.imgpicker-opt-thumb');
            if (thumbEl) thumbEl.src = 'data:image/*;base64,' + data;
          }
        });
      }
      row.addEventListener('click', () => this._select(img.id));
      if (img.id === this.sel.value) row.classList.add('selected');
      this.panel.appendChild(row);
    });
  }

  _makeRow(id, data, name) {
    const row = document.createElement('div');
    row.className = 'imgpicker-opt';
    row.dataset.id = id;

    if (id && data) {
      const thumb = document.createElement('img');
      thumb.className = 'imgpicker-opt-thumb';
      thumb.src = 'data:image/*;base64,' + data;
      row.appendChild(thumb);
    } else if (id) {
      const thumb = document.createElement('img');
      thumb.className = 'imgpicker-opt-thumb imgpicker-opt-thumb-loading';
      row.appendChild(thumb);
    } else {
      const ph = document.createElement('div');
      ph.className = 'imgpicker-opt-nothumb';
      row.appendChild(ph);
    }

    const label = document.createElement('span');
    label.className = 'imgpicker-opt-label';
    label.textContent = name;
    row.appendChild(label);
    return row;
  }

  _select(imgId) {
    this.sel.value = imgId;
    // Fire native change so existing listeners (imgBuildPicItem etc.) still work
    this.sel.dispatchEvent(new Event('change', { bubbles: true }));
    this._renderTrigger();
    this._renderOptions();
    this._close();
    if (this.onChange) this.onChange(imgId);
    // Update the paired large preview if this is a tab picker
    tabPickerUpdatePreview(this.sel.id, imgId);
  }

  _toggle() {
    this.panel.classList.toggle('open');
    this.trigger.classList.toggle('open');
  }

  _close() {
    this.panel.classList.remove('open');
    this.trigger.classList.remove('open');
  }

  // Called after imgLib refreshes — rebuild options and trigger
  refresh() {
    // Preserve current value
    const cur = this.sel.value;
    // Rebuild <option> list in hidden select
    this.sel.innerHTML = '<option value="">— none —</option>';
    imgLib.list.forEach(img => {
      const opt = document.createElement('option');
      opt.value = img.id;
      opt.textContent = img.name;
      this.sel.appendChild(opt);
    });
    if (cur && imgLib.list.find(i => i.id === cur)) this.sel.value = cur;
    else this.sel.value = '';
    this._renderTrigger();
    this._renderOptions();
    tabPickerUpdatePreview(this.sel.id, this.sel.value);
  }

  destroy() {
    document.removeEventListener('click', this._onDocClick);
  }
}

// Registry of active ImgPicker instances keyed by select ID
const imgPickers = {};

// ── Show or hide the large preview below a tab picker ─────────────────────────
function tabPickerUpdatePreview(selectId, imgId) {
  const previewId = TAB_PICKER_PREVIEWS[selectId];
  if (!previewId) return;
  const previewEl = document.getElementById(previewId);
  if (!previewEl) return;
  const imgEl = previewEl.querySelector('img');
  if (!imgId) {
    previewEl.classList.remove('visible');
    imgEl.src = '';
    return;
  }
  previewEl.classList.add('visible');
  if (imgLib.data[imgId]) {
    imgEl.src = 'data:image/*;base64,' + imgLib.data[imgId];
    return;
  }
  GET('/sim/images/' + imgId).then(full => {
    if (full && full.data) {
      imgLib.data[imgId] = full.data;
      imgEl.src = 'data:image/*;base64,' + full.data;
    }
  });
}

// ── Init tab pickers (ANPR + Parking tabs) ────────────────────────────────────
const IMG_SELECTS = [
  'anprImgNormal', 'anprImgVehicle', 'anprImgCutout',
  'parkImgNormal', 'parkImgVehicle',
];

function imgPopulateSelects() {
  IMG_SELECTS.forEach(id => {
    if (imgPickers[id]) {
      imgPickers[id].refresh();
    } else {
      const sel = document.getElementById(id);
      if (!sel) return;
      // Populate hidden select options first
      sel.innerHTML = '<option value="">— none —</option>';
      imgLib.list.forEach(img => {
        const opt = document.createElement('option');
        opt.value = img.id;
        opt.textContent = img.name;
        sel.appendChild(opt);
      });
      imgPickers[id] = new ImgPicker(sel);
    }
  });
}

// ── Resolve an image id → base64 string (fetches if not cached) ───────────────
async function imgResolve(id) {
  if (!id) return null;
  if (imgLib.data[id]) return imgLib.data[id];
  const full = await GET('/sim/images/' + id);
  if (full && full.data) {
    imgLib.data[id] = full.data;
    return full.data;
  }
  return null;
}

// Helper: build a PicItem object { PicName, Content } or null
async function imgBuildPicItem(selectId) {
  const sel = document.getElementById(selectId);
  if (!sel || !sel.value) return null;
  const id = sel.value;
  const meta = imgLib.list.find(i => i.id === id);
  const data = await imgResolve(id);
  if (!data) return null;
  return { PicName: meta ? meta.name : id, Content: data };
}

// ── File input handler ────────────────────────────────────────────────────────
document.getElementById('imgFileInput').addEventListener('change', async (e) => {
  const files = Array.from(e.target.files);
  if (!files.length) return;

  const remaining = 20 - imgLib.list.length;
  const toLoad = files.slice(0, remaining);
  if (toLoad.length < files.length) {
    alert(`Library is limited to 20 images. Loading first ${toLoad.length} file(s).`);
  }

  const entries = await Promise.all(toLoad.map(file => new Promise((resolve) => {
    const reader = new FileReader();
    reader.onload = () => {
      // Strip data-URI prefix — store raw base64
      const raw = reader.result.split(',')[1] || reader.result;
      resolve({ name: file.name, data: raw });
    };
    reader.readAsDataURL(file);
  })));

  const res = await POST('/sim/images', entries);
  // Cache the data we just uploaded so no re-fetch needed
  if (res && res.images) {
    res.images.forEach(img => {
      const match = entries.find(e => e.name === img.name);
      if (match) imgLib.data[img.id] = match.data;
    });
  }
  e.target.value = ''; // allow re-selecting same files
  await imgLoad();
});

// ── Patch buildANPROverrides to include images ────────────────────────────────
// We shadow the original with an async version used by the send button.
async function buildANPROverridesWithImages() {
  const base = buildANPROverrides();
  const normalPic  = await imgBuildPicItem('anprImgNormal');
  const vehiclePic = await imgBuildPicItem('anprImgVehicle');
  const cutoutPic  = await imgBuildPicItem('anprImgCutout');
  if (normalPic)  base.Picture.NormalPic  = normalPic;
  if (vehiclePic) base.Picture.VehiclePic = vehiclePic;
  if (cutoutPic)  base.Picture.CutoutPic  = cutoutPic;
  return base;
}

// ── Patch buildParkingOverrides to include images ─────────────────────────────
async function buildParkingOverridesWithImages() {
  const base = buildParkingOverrides();
  const normalPic  = await imgBuildPicItem('parkImgNormal');
  const vehiclePic = await imgBuildPicItem('parkImgVehicle');
  if (normalPic)  base.Picture.NormalPic  = normalPic;
  if (vehiclePic) base.Picture.VehiclePic = vehiclePic;
  return base;
}

// ── Re-wire ANPR send button to use async version ─────────────────────────────
document.getElementById('btnSendANPR').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const overrides = await buildANPROverridesWithImages();
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/anpr`, { overrides });
  setStatus(res.status);
}, { capture: true }); // capture:true fires before the original listener added earlier

// ── Re-wire Parking send button ───────────────────────────────────────────────
document.getElementById('btnSendParking').addEventListener('click', async () => {
  if (!state.activeCameraId) return;
  const overrides = await buildParkingOverridesWithImages();
  const res = await POST(`/sim/cameras/${state.activeCameraId}/send/parking`, { overrides });
  setStatus(res.status);
}, { capture: true });

// ── Patch msSendEvent to include NormalPic if a global image is selected ──────
// Multi-spot: add a per-spot image picker is complex; instead, a single
// "Scene image for all spots" selector lives at the top of the multispot tab.
// We inject it into index.html programmatically here.
// ── Init image library ────────────────────────────────────────────────────────
imgLoad();

// ── ManSnap LPN Confirmation ────────────────────────────────────────────────
let confirmPollInterval = null;
let currentPendingConfirm = null;

function startConfirmPoll() {
  stopConfirmPoll();
  pollPendingConfirmations();
  confirmPollInterval = setInterval(pollPendingConfirmations, 2000);
}

function stopConfirmPoll() {
  if (confirmPollInterval) {
    clearInterval(confirmPollInterval);
    confirmPollInterval = null;
  }
}

async function pollPendingConfirmations() {
  // don't re-poll if modal is already showing
  if (document.getElementById('confirmModal').style.display !== 'none') return;

  const pendings = await GET('/sim/pending-confirmations');
  if (!Array.isArray(pendings) || pendings.length === 0) return;

  const p = pendings[0];
  currentPendingConfirm = p;
  showConfirmModal(p);
}

function showConfirmModal(p) {
  document.getElementById('confirmCameraLabel').textContent = p.camera_label || p.camera_id;
  document.getElementById('confirmCameraIP').textContent   = p.camera_ip || '—';
  document.getElementById('confirmChannel').textContent    = p.channel !== undefined ? p.channel : '—';
  document.getElementById('confirmDeviceID').textContent   = p.device_id || '—';
  const isManual = p.response_type === 'manual_lpn';
  document.getElementById('confirmResponseType').textContent = isManual ? '🔤 Manual LPN (first capture)' : '📋 With Last Payload';
  document.getElementById('confirmResponseType').style.color = isManual ? 'var(--orange)' : 'var(--green)';
  document.getElementById('confirmLastPlate').textContent = p.last_plate || '—';
  document.getElementById('confirmPayloadPreview').textContent = p.last_payload
    ? JSON.stringify(p.last_payload, null, 2)
    : '—';
  const reuseBtn = document.getElementById('btnConfirmReuse');
  if (isManual || !p.last_plate) {
    reuseBtn.style.display = 'none';
  } else {
    reuseBtn.style.display = '';
  }
  document.getElementById('confirmPlateInput').value = p.last_plate || '';
  document.getElementById('confirmPlateInput').focus();
  document.getElementById('confirmModal').style.display = 'flex';
}

function closeConfirmModal() {
  document.getElementById('confirmModal').style.display = 'none';
  currentPendingConfirm = null;
}

async function doConfirm(mode) {
  if (!currentPendingConfirm) return;
  const plate = document.getElementById('confirmPlateInput').value.trim();
  if (!plate) {
    document.getElementById('confirmPlateInput').focus();
    document.getElementById('confirmPlateInput').style.borderColor = 'var(--red)';
    setTimeout(() => {
      document.getElementById('confirmPlateInput').style.borderColor = '';
    }, 1000);
    return;
  }
  const res = await POST('/sim/confirm-lpn', {
    camera_id:    currentPendingConfirm.camera_id,
    plate_number: plate,
    mode:         mode,
  });
  if (res.confirmed) {
    setStatus(200);
  } else {
    setStatus(0);
  }
  closeConfirmModal();
}

document.getElementById('btnConfirmReuse').addEventListener('click', () => doConfirm('reuse_last'));
document.getElementById('btnConfirmFake').addEventListener('click', () => doConfirm('fake'));

document.getElementById('btnCancelConfirm').addEventListener('click', () => {
  closeConfirmModal();
});

// Close modal on backdrop click
document.querySelector('.modal-backdrop')?.addEventListener('click', () => {
  closeConfirmModal();
});

// Enter key in plate input triggers reuse (default action)
document.getElementById('confirmPlateInput').addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    const reuseBtn = document.getElementById('btnConfirmReuse');
    if (reuseBtn.style.display !== 'none') {
      reuseBtn.click();
    } else {
      document.getElementById('btnConfirmFake').click();
    }
  }
});

// Start polling on page load
startConfirmPoll();