class CdgRenderer {
  constructor(canvas) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d', { alpha: false });
    this.indices = new Uint8Array(288 * 192);
    this.palette = Array.from({ length: 16 }, () => [0, 0, 0]);
    this.image = this.ctx.createImageData(288, 192);
    this.data = null;
    this.trackID = 0;
    this.requestID = 0;
    this.packetIndex = 0;
    this.lastPosition = 0;
    this.loadNumber = 0;
  }

  async setTrack(trackID, requestID = 0) {
    if (trackID === this.trackID && requestID === this.requestID) return;
    this.trackID = trackID;
    this.requestID = requestID;
    this.reset();
    this.data = null;
    const currentLoad = ++this.loadNumber;
    if (!trackID) return;
    try {
      const mediaBase=requestID?`/request-media/${requestID}`:`/media/${trackID}`;
      const response = await fetch(`${mediaBase}/cdg`);
      if (!response.ok) throw new Error('No se pudo cargar el CDG');
      const data = new Uint8Array(await response.arrayBuffer());
      if (currentLoad !== this.loadNumber) return;
      this.data = data;
      this.packets = Math.floor(data.length / 24);
    } catch (error) {
      const message = document.querySelector('#message');
      if (message) message.textContent = error.message;
    }
  }

  reset() {
    this.indices.fill(0);
    this.palette = Array.from({ length: 16 }, () => [0, 0, 0]);
    this.packetIndex = 0;
    this.lastPosition = 0;
  }

  draw(position) {
    if (!this.data) return;
    if (position + 0.03 < this.lastPosition) this.reset();
    const target = Math.min(this.packets, Math.max(0, Math.floor(position * 300)));
    while (this.packetIndex < target) {
      const start = this.packetIndex * 24;
      this.applyPacket(this.data.subarray(start, start + 24));
      this.packetIndex++;
    }
    this.lastPosition = position;
    const out = this.image.data;
    for (let i = 0, p = 0; i < this.indices.length; i++, p += 4) {
      const color = this.palette[this.indices[i]];
      out[p] = color[0]; out[p + 1] = color[1]; out[p + 2] = color[2]; out[p + 3] = 255;
    }
    this.ctx.putImageData(this.image, 0, 0);
  }

  applyPacket(packet) {
    if ((packet[0] & 0x3f) !== 9) return;
    const instruction = packet[1] & 0x3f;
    const d = Array.from(packet.subarray(4, 20), x => x & 0x3f);
    if (instruction === 1) {
      this.indices.fill(d[0] & 0x0f);
      return;
    }
    if (instruction === 30 || instruction === 31) {
      const first = instruction === 30 ? 0 : 8;
      for (let i = 0; i < 8; i++) {
        const value = (d[i * 2] << 6) | d[i * 2 + 1];
        this.palette[first + i] = [((value >> 8) & 15) * 17, ((value >> 4) & 15) * 17, (value & 15) * 17];
      }
      return;
    }
    if (instruction === 6 || instruction === 38) {
      const color0 = d[0] & 15, color1 = d[1] & 15;
      const row = d[2] & 31, col = d[3] & 63;
      if (row >= 18 || col >= 50) return;
      const x0 = col * 6 - 6, y0 = row * 12 - 12;
      for (let y = 0; y < 12; y++) {
        const bits = d[4 + y] & 0x3f;
        for (let x = 0; x < 6; x++) {
          const px = x0 + x, py = y0 + y;
          if (px < 0 || px >= 288 || py < 0 || py >= 192) continue;
          const color = (bits & (1 << (5 - x))) === 0 ? color0 : color1;
          const at = py * 288 + px;
          this.indices[at] = instruction === 38 ? (this.indices[at] ^ color) : color;
        }
      }
    }
  }
}

const role = document.body.dataset.role;
const statusNode = document.querySelector('#player-status');
const audio = document.querySelector('#audio');
const canvas = document.querySelector('#cdg');
const renderer = canvas ? new CdgRenderer(canvas) : null;
let state = { status: 'stopped', trackId: 0, offset: 0, serverNow: Date.now() };
let stateReceivedAt = performance.now();
let audioEnabled = false;

function estimatedPosition() {
  if (state.status !== 'playing') return state.offset;
  return state.offset + (performance.now() - stateReceivedAt) / 1000;
}

function applyState(next) {
  const previous = state;
  const trackChanged = next.trackId !== state.trackId || next.mediaRequestId !== state.mediaRequestId;
  state = next;
  stateReceivedAt = performance.now();
  if (statusNode) statusNode.textContent = state.status === 'announcing' || state.status === 'announcement-paused'
    ? `Sigue ${state.nextRequester}: ${state.nextTitle} · ${state.status === 'announcing' ? `${state.countdown}s` : 'en pausa'}`
    : `${state.status} · ${formatTime(state.offset)}`;
  if (renderer && trackChanged) renderer.setTrack(state.trackId,state.mediaRequestId||0);

  if (trackChanged) {
    const title = document.querySelector('#song-title');
    if (title) title.textContent = state.title || 'KaraokeList · Proyector';
    const artist = document.querySelector('#song-artist');
    if (artist) artist.textContent = state.artist || '';
    if (role === 'projector' && audio && state.trackId) {
      audio.src = state.mediaRequestId?`/request-media/${state.mediaRequestId}/mp3`:`/media/${state.trackId}/mp3`;
      audio.load();
    }
  }
  const dedication = document.querySelector('#dedication');
  if (dedication) {
    dedication.textContent = state.message ? `${state.requester}: ${state.message}` : '';
    dedication.hidden = !state.message;
  }
  const announcement=document.querySelector('#next-announcement');
  if(announcement){
    const active=state.status==='announcing'||state.status==='announcement-paused';
    const artist=state.nextArtist?` — ${state.nextArtist}`:'';
    announcement.textContent=active?`Ahora sigue ${state.nextRequester} con ${state.nextTitle}${artist}. ${state.status==='announcing'?`Comienza en ${state.countdown} segundos.`:'En pausa.'}`:'';
    announcement.hidden=!active;
  }

  if (role === 'projector' && audioEnabled && audio) {
    if (Math.abs(audio.currentTime - state.offset) > 0.8) audio.currentTime = state.offset;
    if (state.status === 'playing' && (previous.status !== 'playing' || trackChanged)) {
      audio.play().catch(() => showMessage('El navegador bloqueó el audio. Pulsa “Habilitar audio” otra vez.'));
    } else if (state.status !== 'playing' && !audio.paused) {
      audio.pause();
    }
    if (state.status === 'stopped') audio.currentTime = 0;
  }
}

function formatTime(seconds) {
  const value = Math.max(0, Math.floor(seconds || 0));
  return `${String(Math.floor(value / 60)).padStart(2, '0')}:${String(value % 60).padStart(2, '0')}`;
}

function showMessage(value) {
  const message = document.querySelector('#message');
  if (message) message.textContent = value;
}

if (audio) {
  audio.addEventListener('loadedmetadata', () => {
    if (state.status === 'playing') audio.currentTime = estimatedPosition();
  });
}

if (renderer) {
  function frame() {
    const position = role === 'projector' && audioEnabled && audio && !audio.paused ? audio.currentTime : estimatedPosition();
    renderer.draw(position);
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
}

const events = new EventSource('/api/events');
events.onmessage = event => applyState(JSON.parse(event.data));
events.onerror = () => { if (statusNode) statusNode.textContent = 'Reconectando…'; };

const enableAudio = document.querySelector('#enable-audio');
if (enableAudio) {
  enableAudio.addEventListener('click', async () => {
    audioEnabled = true;
    try {
      if (state.status === 'playing') {
        audio.currentTime = estimatedPosition();
        await audio.play();
      }
      enableAudio.hidden = true;
      showMessage('Audio habilitado.');
    } catch (_) {
      audioEnabled = false;
      showMessage('No se pudo habilitar el audio. Intenta de nuevo.');
    }
  });
}

const fullscreen = document.querySelector('#fullscreen');
if (fullscreen) fullscreen.addEventListener('click', async () => {
  const stage = document.querySelector('#viewer-stage');
  try {
    if (!document.fullscreenElement) await stage.requestFullscreen();
    else await document.exitFullscreen();
  } catch (_) {
    showMessage('Este navegador no permitió el modo de pantalla completa.');
  }
});

const tokenInput = document.querySelector('#admin-token');
if (tokenInput) tokenInput.value = sessionStorage.getItem('karaokeAdminToken') || '';

async function sendAdmin(path, payload) {
  const token = tokenInput ? tokenInput.value.trim() : '';
  sessionStorage.setItem('karaokeAdminToken', token);
  return fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Admin-Token': token }, body: JSON.stringify(payload) });
}

const clearPartyButton=document.querySelector('#clear-party');
if(clearPartyButton)clearPartyButton.addEventListener('click',async()=>{
  if(!confirm('¿Vaciar la cola, borrar todos los mensajes y saludos, y detener la canción actual? Esta acción no se puede deshacer.'))return;
  const status=document.querySelector('#clear-status');
  clearPartyButton.disabled=true;status.textContent='Limpiando cola y mensajes…';
  try{
    const response=await sendAdmin('/api/admin/clear-party',{});
    if(!response.ok)throw new Error(response.status===401?'Token admin inválido':await response.text());
    applyState(await response.json());refreshQueue();refreshGreetings();refreshTicker();
    status.textContent='Cola, mensajes y saludos eliminados. El catálogo sigue disponible.';
  }catch(error){status.textContent=error.message;}
  finally{clearPartyButton.disabled=false;}
});

const indexButton = document.querySelector('#index-library');
if (indexButton) indexButton.addEventListener('click', async () => {
  const status = document.querySelector('#index-status');
  indexButton.disabled = true;
  status.textContent = 'Indexando CDG+MP3 y leyendo tags…';
  try {
    const response = await sendAdmin('/api/admin/index', {});
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'Falló la indexación');
    status.textContent = `Listo: ${result.Pairs} pares; ${result.CDGOnly} CDG sin MP3; ${result.MP3Only} MP3 sin CDG; ${result.EmptyCDG} CDG vacíos omitidos; ${result.UnreadableID} tags no legibles.`;
    setTimeout(() => location.reload(), 1200);
  } catch (error) {
    status.textContent = error.message;
    indexButton.disabled = false;
  }
});

const delayButton=document.querySelector('#save-transition-delay');
if(delayButton)delayButton.addEventListener('click',async()=>{
  const input=document.querySelector('#transition-delay');
  const status=document.querySelector('#transition-delay-status');
  const seconds=Number(input.value);
  if(!Number.isInteger(seconds)||seconds<0||seconds>120){status.textContent='Usa un valor entero entre 0 y 120.';return;}
  delayButton.disabled=true;
  try{
    const response=await sendAdmin('/api/admin/transition-delay',{seconds});
    if(!response.ok)throw new Error(response.status===401?'Token admin inválido':await response.text());
    status.textContent=`Espera guardada: ${seconds} segundos.`;
  }catch(error){status.textContent=error.message;}
  finally{delayButton.disabled=false;}
});

document.querySelectorAll('[data-action]').forEach(button => button.addEventListener('click', async () => {
  const select = document.querySelector('#track-id');
  const payload = { action: button.dataset.action, trackId: select ? Number(select.value || 0) : 0 };
  const response = await sendAdmin('/api/control', payload);
  if (!response.ok) {
    if (statusNode) statusNode.textContent = response.status === 401 ? 'Token admin inválido' : await response.text();
    return;
  }
  applyState(await response.json());
  refreshQueue();
}));

const nameDialog = document.querySelector('#name-dialog');
const requestDialog = document.querySelector('#request-dialog');
let selectedTrack = 0;
let requesterName = '';
let pendingGreeting = false;
async function loadProfile() {
  if (!nameDialog) return;
  try { const response=await fetch('/api/profile'); const profile=await response.json(); requesterName=profile.name||''; document.querySelector('#requester-name').value=requesterName; if(!requesterName||new URLSearchParams(location.search).has('profile')) openProfile(); }
  catch (_) { nameDialog.showModal(); }
}
function openProfile() {
  if (!nameDialog) return;
  document.querySelector('#requester-name').value=requesterName;
  const title=document.querySelector('#name-dialog-title');
  if(title)title.textContent=requesterName?'Cambiar nombre':'¿Cómo te llamas?';
  nameDialog.showModal();
}
document.querySelector('#change-name')?.addEventListener('click',openProfile);
if (nameDialog) {
  loadProfile();
  document.querySelector('#name-form').addEventListener('submit', async event => {
    event.preventDefault();
    const error=document.querySelector('#name-error');
    const response=await fetch('/api/profile',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:document.querySelector('#requester-name').value})});
    if(!response.ok){error.textContent=await response.text();return;}
    requesterName=(await response.json()).name; nameDialog.close();
    if(pendingGreeting){pendingGreeting=false;await submitGreeting();}
    else if(selectedTrack) requestDialog.showModal();
  });
}
document.querySelectorAll('.request-song').forEach(button => button.addEventListener('click', async () => {
  selectedTrack=Number(button.dataset.trackId);
  document.querySelector('#request-title').textContent=button.dataset.title;
  document.querySelector('#request-artist').textContent=button.dataset.artist || 'Artista sin identificar';
  document.querySelector('#request-message').value='';
  document.querySelector('#request-status').textContent='';
  if(!requesterName && nameDialog){ nameDialog.showModal(); return; }
  requestDialog.showModal();
}));
const requestForm=document.querySelector('#request-form');
if(requestForm) requestForm.addEventListener('submit',async event=>{
  event.preventDefault();
  const status=document.querySelector('#request-status');
  const response=await fetch('/api/requests',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({trackId:selectedTrack,message:document.querySelector('#request-message').value})});
  if(!response.ok){status.textContent=await response.text();return;}
  requestDialog.close(); selectedTrack=0;
  const feedback=document.querySelector('#request-feedback');
  if(feedback)feedback.textContent='¡Canción agregada a la lista!';
});
document.querySelectorAll('[data-close-dialog]').forEach(button=>button.addEventListener('click',()=>requestDialog.close()));

async function refreshQueue(){
  const node=document.querySelector('#request-queue'); if(!node)return;
  try { const response=await fetch('/api/requests',{headers:{'X-Admin-Token':tokenInput?tokenInput.value.trim():''}}); if(!response.ok)throw new Error(); const items=await response.json();
    node.innerHTML=items.length?items.map((item,index)=>`<article class="queue-item"><div><strong>${escapeHTML(item.artist ? `${item.artist} — ${item.title}` : item.title)}</strong><small>${escapeHTML(item.requester)}${item.message?` · “${escapeHTML(item.message)}”`:''} · ${item.status==='playing'?'Reproduciendo':`#${index+1}`}</small></div>${item.status==='queued'?`<button class="button secondary" data-play-request="${item.id}">Reproducir</button>`:''}</article>`).join(''):'<p class="note">No hay canciones pendientes.</p>';
    node.querySelectorAll('[data-play-request]').forEach(button=>button.addEventListener('click',async()=>{const response=await sendAdmin('/api/control',{action:'play',requestId:Number(button.dataset.playRequest)});if(response.ok)applyState(await response.json());refreshQueue();}));
  } catch (_) { node.textContent='No se pudo cargar la cola.'; }
}
function escapeHTML(value){return String(value).replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));}
if(document.querySelector('#request-queue')){refreshQueue();setInterval(refreshQueue,3000);}

const greetingList=document.querySelector('#greeting-list');
async function refreshGreetings(){if(!greetingList)return;try{const response=await fetch('/api/greetings');const items=await response.json();greetingList.innerHTML=items.length?items.map(item=>`<article class="greeting-card"><p>“${escapeHTML(item.message)}”</p><strong>${escapeHTML(item.name)}</strong><small>${item.source==='song'?` · Dedicado a ${escapeHTML(item.title)}${item.artist?` — ${escapeHTML(item.artist)}`:''}`:' · Saludo para todos'}</small></article>`).join(''):'<section class="empty-state"><strong>Aún no hay saludos.</strong><span>Escribe un saludo aquí o agrega una dedicatoria al pedir una canción.</span></section>'; }catch(_){greetingList.textContent='No se pudieron cargar los saludos.';}}
if(greetingList){refreshGreetings();setInterval(refreshGreetings,4000);}

async function submitGreeting(){
  const status=document.querySelector('#greeting-status');
  const message=document.querySelector('#greeting-message');
  if(!status||!message)return;
  const response=await fetch('/api/greetings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({message:message.value})});
  if(!response.ok){status.textContent=await response.text();return;}
  message.value='';status.textContent='Tu saludo ya aparece en el proyector.';refreshGreetings();
}
const greetingForm=document.querySelector('#greeting-form');
if(greetingForm)greetingForm.addEventListener('submit',async event=>{
  event.preventDefault();
  if(!requesterName){pendingGreeting=true;openProfile();return;}
  await submitGreeting();
});

const ticker=document.querySelector('#ticker-track');
let tickerContent='';
async function refreshTicker(){
  if(!ticker)return;
  try{
    const response=await fetch('/api/ticker');if(!response.ok)return;
    const items=(await response.json()).reverse();
    const content=items.map(item=>`${item.name}: ${item.message}`).join('　　　✦　　　');
    if(content===tickerContent)return;tickerContent=content;
    ticker.innerHTML=content?`<span>${escapeHTML(content)}</span><span aria-hidden="true">${escapeHTML(content)}</span>`:'';
    ticker.parentElement.hidden=!content;
  }catch(_){}
}
if(ticker){refreshTicker();setInterval(refreshTicker,8000);}
