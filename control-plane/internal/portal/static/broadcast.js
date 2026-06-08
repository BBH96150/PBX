/* Paging Broadcast PWA.
 *
 * Records a short clip from the device mic, encodes it as a mono 16-bit PCM
 * WAV (FreeSWITCH plays it natively, resampling as needed), and uploads it to
 * the server, which pages it to every member of the chosen group.
 *
 * Recording uses getUserMedia + a ScriptProcessor tap (works across browsers
 * incl. iOS Safari) rather than MediaRecorder, so we get raw PCM and avoid
 * server-side transcoding of webm/opus.
 */
(function () {
  'use strict';

  var groups = Array.isArray(window.__PAGING_GROUPS__) ? window.__PAGING_GROUPS__ : [];

  var els = {
    group: document.getElementById('group'),
    rec: document.getElementById('rec'),
    send: document.getElementById('send'),
    discard: document.getElementById('discard'),
    status: document.getElementById('status'),
    timer: document.getElementById('timer'),
    level: document.getElementById('level'),
    player: document.getElementById('player'),
  };

  var MAX_SECONDS = 120;
  var audioCtx, mediaStream, source, processor, analyser;
  var chunks = [];
  var sampleRate = 48000;
  var recording = false;
  var startedAt = 0, timerInt = 0, levelRaf = 0;
  var lastBlob = null;

  function setStatus(msg, kind) {
    els.status.textContent = msg || '';
    els.status.className = 'status' + (kind ? ' ' + kind : '');
  }

  function fillGroups() {
    if (!groups.length) {
      els.group.innerHTML = '<option value="">No paging groups</option>';
      els.group.disabled = true;
      els.rec.disabled = true;
      setStatus('No paging groups in your tenant yet. Create one in the portal first.', 'warn');
      return;
    }
    els.group.innerHTML = groups.map(function (g) {
      var label = g.name + ' · ' + (g.members || 0) + ' member' + (g.members === 1 ? '' : 's') +
        (g.enabled ? '' : ' (disabled)');
      return '<option value="' + g.id + '">' + escapeHtml(label) + '</option>';
    }).join('');
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c];
    });
  }

  function fmtTime(sec) {
    var m = Math.floor(sec / 60), s = Math.floor(sec % 60);
    return m + ':' + (s < 10 ? '0' : '') + s;
  }

  async function startRecording() {
    if (recording) return;
    try {
      mediaStream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      });
    } catch (e) {
      setStatus('Microphone permission denied or unavailable.', 'err');
      return;
    }
    audioCtx = new (window.AudioContext || window.webkitAudioContext)();
    sampleRate = audioCtx.sampleRate;
    source = audioCtx.createMediaStreamSource(mediaStream);
    analyser = audioCtx.createAnalyser();
    analyser.fftSize = 512;
    processor = audioCtx.createScriptProcessor(4096, 1, 1);
    chunks = [];

    processor.onaudioprocess = function (ev) {
      var input = ev.inputBuffer.getChannelData(0);
      chunks.push(new Float32Array(input));
    };
    source.connect(analyser);
    source.connect(processor);
    processor.connect(audioCtx.destination);

    recording = true;
    startedAt = Date.now();
    lastBlob = null;
    els.player.style.display = 'none';
    els.send.disabled = true;
    els.discard.disabled = true;
    els.group.disabled = true;
    els.rec.classList.add('recording');
    els.rec.querySelector('.rec-label').textContent = 'Stop';
    setStatus('Recording… speak your page, then tap Stop.', '');
    tick();
    drawLevel();
  }

  function tick() {
    timerInt = setInterval(function () {
      var sec = (Date.now() - startedAt) / 1000;
      els.timer.textContent = fmtTime(sec);
      if (sec >= MAX_SECONDS) stopRecording();
    }, 250);
  }

  function drawLevel() {
    var buf = new Uint8Array(analyser.frequencyBinCount);
    function loop() {
      if (!recording) return;
      analyser.getByteTimeDomainData(buf);
      var peak = 0;
      for (var i = 0; i < buf.length; i++) {
        var v = Math.abs(buf[i] - 128);
        if (v > peak) peak = v;
      }
      els.level.style.width = Math.min(100, (peak / 128) * 140) + '%';
      levelRaf = requestAnimationFrame(loop);
    }
    loop();
  }

  function stopRecording() {
    if (!recording) return;
    recording = false;
    clearInterval(timerInt);
    cancelAnimationFrame(levelRaf);
    els.level.style.width = '0%';
    try { processor.disconnect(); source.disconnect(); analyser.disconnect(); } catch (e) {}
    if (mediaStream) mediaStream.getTracks().forEach(function (t) { t.stop(); });
    if (audioCtx) audioCtx.close();

    var wav = encodeWav(chunks, sampleRate);
    lastBlob = wav;
    els.player.src = URL.createObjectURL(wav);
    els.player.style.display = 'block';
    els.rec.classList.remove('recording');
    els.rec.querySelector('.rec-label').textContent = 'Record';
    els.send.disabled = false;
    els.discard.disabled = false;
    els.group.disabled = false;
    setStatus('Recorded ' + els.timer.textContent + '. Review, then Broadcast.', 'ok');
  }

  function discard() {
    lastBlob = null;
    els.player.style.display = 'none';
    els.player.removeAttribute('src');
    els.timer.textContent = '0:00';
    els.send.disabled = true;
    els.discard.disabled = true;
    setStatus('Discarded.', '');
  }

  function encodeWav(buffers, rate) {
    var len = 0, i;
    for (i = 0; i < buffers.length; i++) len += buffers[i].length;
    var pcm = new Float32Array(len), off = 0;
    for (i = 0; i < buffers.length; i++) { pcm.set(buffers[i], off); off += buffers[i].length; }

    var bytes = 44 + pcm.length * 2;
    var ab = new ArrayBuffer(bytes);
    var dv = new DataView(ab);
    function ws(o, s) { for (var j = 0; j < s.length; j++) dv.setUint8(o + j, s.charCodeAt(j)); }
    ws(0, 'RIFF'); dv.setUint32(4, bytes - 8, true); ws(8, 'WAVE');
    ws(12, 'fmt '); dv.setUint32(16, 16, true); dv.setUint16(20, 1, true);
    dv.setUint16(22, 1, true); dv.setUint32(24, rate, true);
    dv.setUint32(28, rate * 2, true); dv.setUint16(32, 2, true); dv.setUint16(34, 16, true);
    ws(36, 'data'); dv.setUint32(40, pcm.length * 2, true);
    var p = 44;
    for (i = 0; i < pcm.length; i++) {
      var v = Math.max(-1, Math.min(1, pcm[i]));
      dv.setInt16(p, v < 0 ? v * 0x8000 : v * 0x7fff, true);
      p += 2;
    }
    return new Blob([ab], { type: 'audio/wav' });
  }

  async function broadcast() {
    if (!lastBlob) return;
    var gid = els.group.value;
    if (!gid) { setStatus('Pick a group first.', 'warn'); return; }
    els.send.disabled = true;
    setStatus('Broadcasting…', '');
    var fd = new FormData();
    fd.append('group_id', gid);
    fd.append('audio', lastBlob, 'page.wav');
    try {
      var res = await fetch('/admin/broadcast/send', { method: 'POST', body: fd, credentials: 'same-origin' });
      var data = await res.json().catch(function () { return {}; });
      if (!res.ok) {
        setStatus(data.error || ('Broadcast failed (' + res.status + ')'), 'err');
        els.send.disabled = false;
        return;
      }
      setStatus('Paged ' + data.paged + ' of ' + data.members + ' in “' + data.group + '”.', 'ok');
      discard();
    } catch (e) {
      setStatus('Network error — try again.', 'err');
      els.send.disabled = false;
    }
  }

  els.rec.addEventListener('click', function () { recording ? stopRecording() : startRecording(); });
  els.send.addEventListener('click', broadcast);
  els.discard.addEventListener('click', discard);

  fillGroups();

  // ---------------------------------------------------------------------------
  // Live push-to-talk (SIP.js over wss → page code → conference paging).
  // Press = INVITE the group's page code; release = hang up. The pager is the
  // (unmuted) conference moderator; members auto-answer muted and listen.
  // ---------------------------------------------------------------------------
  var exts = Array.isArray(window.__EXTS__) ? window.__EXTS__ : [];
  var wsURL = window.__WSURL__ || '';
  var L = {
    tabRecord: document.getElementById('tab-record'),
    tabLive: document.getElementById('tab-live'),
    modeRecord: document.getElementById('mode-record'),
    modeLive: document.getElementById('mode-live'),
    ext: document.getElementById('live-ext'),
    golive: document.getElementById('golive'),
    connect: document.getElementById('live-connect'),
    stage: document.getElementById('live-stage'),
    ptt: document.getElementById('ptt'),
    audio: document.getElementById('live-audio'),
  };
  var ua = null, registerer = null, pttSession = null, creds = null, registered = false;

  function showMode(live) {
    if (!L.tabRecord) return;
    L.tabRecord.classList.toggle('active', !live);
    L.tabLive.classList.toggle('active', live);
    L.modeRecord.style.display = live ? 'none' : '';
    L.modeLive.style.display = live ? '' : 'none';
  }
  if (L.tabRecord) L.tabRecord.addEventListener('click', function () { showMode(false); });
  if (L.tabLive) L.tabLive.addEventListener('click', function () { showMode(true); });

  if (L.ext) {
    L.ext.innerHTML = exts.map(function (e) {
      return '<option value="' + e.id + '">' + escapeHtml(e.extension + (e.display_name ? ' — ' + e.display_name : '')) + '</option>';
    }).join('');
  }

  function currentGroup() {
    var id = els.group ? els.group.value : '';
    for (var i = 0; i < groups.length; i++) if (groups[i].id === id) return groups[i];
    return null;
  }

  async function goLive() {
    if (!exts.length || typeof SIP === 'undefined') { setStatus('Live mode unavailable.', 'err'); return; }
    L.golive.disabled = true;
    setStatus('Issuing credentials…', '');
    var fd = new FormData(); fd.append('extension_id', L.ext.value);
    try {
      var r = await fetch('/admin/softphone/credentials', { method: 'POST', body: fd, credentials: 'same-origin' });
      if (!r.ok) throw new Error('cred ' + r.status);
      creds = await r.json();
    } catch (e) { setStatus('Credential error.', 'err'); L.golive.disabled = false; return; }
    setStatus('Connecting…', '');
    try {
      ua = new SIP.UserAgent({
        uri: SIP.UserAgent.makeURI('sip:' + creds.username + '@' + creds.domain),
        authorizationUsername: creds.username,
        authorizationPassword: creds.password,
        displayName: creds.display_name || creds.extension,
        transportOptions: { server: wsURL },
        sessionDescriptionHandlerFactoryOptions: {
          peerConnectionConfiguration: { iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] },
        },
        logBuiltinEnabled: false, logConfiguration: false,
      });
      await ua.start();
      registerer = new SIP.Registerer(ua);
      registerer.stateChange.addListener(function (st) {
        if (st === 'Registered' && !registered) {
          registered = true;
          L.connect.style.display = 'none';
          L.stage.style.display = '';
          setStatus('Live — hold the button to page.', 'ok');
        }
      });
      await registerer.register();
    } catch (e) { setStatus('Connect failed: ' + e.message, 'err'); L.golive.disabled = false; }
  }
  if (L.golive) L.golive.addEventListener('click', goLive);

  function attachLiveAudio(s) {
    var pc = s.sessionDescriptionHandler && s.sessionDescriptionHandler.peerConnection;
    if (!pc) return;
    var ms = new MediaStream();
    pc.getReceivers().forEach(function (rr) { if (rr.track) ms.addTrack(rr.track); });
    L.audio.srcObject = ms;
  }

  async function pttStart() {
    if (!ua || !registered || pttSession) return;
    var g = currentGroup();
    if (!g) { setStatus('Pick a group first.', 'warn'); return; }
    if (!g.code) { setStatus('“' + g.name + '” has no page code — use Record mode.', 'warn'); return; }
    L.ptt.classList.add('talking');
    L.ptt.querySelector('.rec-label').textContent = 'Talking…';
    setStatus('Paging ' + g.name + '…', '');
    var uri = SIP.UserAgent.makeURI('sip:' + g.code + '@' + creds.domain);
    var inv = new SIP.Inviter(ua, uri, { sessionDescriptionHandlerOptions: { constraints: { audio: true, video: false } } });
    pttSession = inv;
    inv.stateChange.addListener(function (st) {
      if (st === 'Established') attachLiveAudio(inv);
      if (st === 'Terminated') pttSession = null;
    });
    try { await inv.invite(); } catch (e) { setStatus('Page failed: ' + e.message, 'err'); pttEnd(); }
  }

  function pttEnd() {
    if (L.ptt) {
      L.ptt.classList.remove('talking');
      L.ptt.querySelector('.rec-label').textContent = 'Hold to Talk';
    }
    var s = pttSession; pttSession = null;
    if (s) {
      try {
        if (s.state === 'Established') s.bye();
        else if (s.state === 'Establishing' || s.state === 'Initial') s.cancel();
      } catch (e) {}
    }
    if (registered) setStatus('Live — hold the button to page.', '');
  }

  if (L.ptt) {
    L.ptt.addEventListener('pointerdown', function (ev) { ev.preventDefault(); pttStart(); });
    L.ptt.addEventListener('pointerup', function (ev) { ev.preventDefault(); pttEnd(); });
    L.ptt.addEventListener('pointercancel', pttEnd);
    L.ptt.addEventListener('pointerleave', function () { if (pttSession) pttEnd(); });
  }

  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/admin/broadcast.sw.js', { scope: '/admin/broadcast' }).catch(function () {});
  }
})();
