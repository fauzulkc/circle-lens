import { cleanSettings } from '../../shared/lens-settings.js';

const api = globalThis.browser ?? globalThis.chrome;
const form = document.querySelector('form');
const fields = {
  enabled: form.elements.namedItem('enabled'),
  loops: form.elements.namedItem('loops'),
  zoom: form.elements.namedItem('zoom'),
  radius: form.elements.namedItem('radius'),
  gap: form.elements.namedItem('gap'),
};

const show = (settings) => {
  fields.enabled.checked = settings.enabled;
  fields.loops.value = String(settings.loops);
  fields.zoom.value = String(settings.zoom);
  fields.radius.value = String(settings.radius);
  fields.gap.value = String(settings.gap);
  form.querySelector('[data-for=gap]').textContent = `${settings.gap} s`;
  form.querySelector('[data-for=loops]').textContent = `${settings.loops} circle${settings.loops === 1 ? '' : 's'}`;
  form.querySelector('[data-for=zoom]').textContent = `${settings.zoom.toFixed(1)}×`;
  form.querySelector('[data-for=radius]').textContent = `${settings.radius * 2} px`;
  for (const name of ['loops', 'zoom', 'radius', 'gap']) fields[name].disabled = !settings.enabled;
};

const read = () =>
  cleanSettings({
    enabled: fields.enabled.checked,
    loops: Number(fields.loops.value),
    zoom: Number(fields.zoom.value),
    radius: Number(fields.radius.value),
    gap: Number(fields.gap.value),
  });

api.storage.sync.get(null).then((stored) => show(cleanSettings(stored)));
form.addEventListener('input', () => {
  const settings = read();
  show(settings);
  api.storage.sync.set(settings);
});
