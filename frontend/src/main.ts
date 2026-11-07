import './style.css';
import { PackWithProgress, CancelPack, Info, List, Extract, Edit, Verify, SelectDirectory, SelectFile, SaveFile, SelectSaveDir, SelectIconFile, GetPackageInfo, GetArchiveIcon, CountFiles, GetFileSize } from '../wailsjs/go/main/App';
import { EventsOn, EventsOff, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';

type PackRequest = Parameters<typeof PackWithProgress>[2];

let isRunning = false;
let cancelRequested = false;
let selectedArchivePath = '';
let selectedIconPath = '';
let archivePackages: Array<{ index: number; kind: string; titleId: string; version: number }> = [];

function setActivity(title: string, detail: string, state: 'ready' | 'running' | 'success' | 'error' = 'success') {
  const panel = document.getElementById('activity-content');
  if (!panel) return;
  panel.innerHTML = '';
  const mark = document.createElement('span');
  mark.className = 'activity-empty-mark';
  mark.textContent = state === 'error' ? '!' : state === 'running' ? '…' : state === 'success' ? '✓' : '✳';
  const heading = document.createElement('strong');
  heading.textContent = title;
  const text = document.createElement('p');
  text.textContent = detail;
  panel.append(mark, heading, text);
  panel.dataset.state = state;
}

function setArchivePath(path: string) {
  selectedArchivePath = path;
  document.querySelectorAll<HTMLInputElement>('#info-file, #list-file, #extract-file, #edit-file, #verify-file').forEach(input => { input.value = path; });
  const label = document.getElementById('selected-archive-label');
  if (label) label.textContent = path || 'No archive selected';
}

async function loadArchive(path: string) {
  if (!path) return;
  setArchivePath(path);
  selectedIconPath = '';
  const output = document.getElementById('info-output');
  const pkgSelect = document.getElementById('archive-package') as HTMLSelectElement;
  try {
      const info = await Info(path);
    const infoLines = info.split('\n').filter(line => /^\[\d+\]/.test(line));
    archivePackages = infoLines.map(line => {
      const match = line.match(/^\[(\d+)\]\s+(\S+)\s+(\S+)\s+(\S+)/);
      return match ? { index: Number(match[1]), kind: match[2], titleId: match[3], version: Number(match[4].replace(/^v0x/, '')) } : null;
    }).filter((pkg): pkg is NonNullable<typeof pkg> => pkg !== null);
    pkgSelect.replaceChildren(...archivePackages.map(pkg => {
      const option = document.createElement('option');
      option.value = String(pkg.index);
      option.textContent = `[${pkg.index}] ${pkg.kind} ${pkg.titleId}`;
      return option;
    }));
    pkgSelect.disabled = archivePackages.length === 0;
    (document.getElementById('edit-btn') as HTMLButtonElement).disabled = archivePackages.length === 0;
    if (!archivePackages.length) pkgSelect.add(new Option('No packages found', ''));
    if (output) output.textContent = info;
    const initialIndex = Math.max(0, archivePackages.findIndex(pkg => pkg.kind === 'base'));
    pkgSelect.value = String(archivePackages[initialIndex]?.index ?? 0);
    await refreshArchiveIcon();
    setActivity('Archive opened', `${path}\n${archivePackages.length} package(s) ready.`);
  } catch (e: any) {
    archivePackages = [];
    pkgSelect.replaceChildren(new Option('Could not read packages', ''));
    pkgSelect.disabled = true;
    (document.getElementById('edit-btn') as HTMLButtonElement).disabled = true;
    if (output) output.textContent = `Error: ${e}`;
    setActivity('Archive open failed', String(e), 'error');
  }
}

async function refreshArchiveIcon() {
  const preview = document.getElementById('archive-icon-preview') as HTMLImageElement;
  const empty = document.getElementById('archive-icon-empty');
  const pkg = document.getElementById('archive-package') as HTMLSelectElement;
  if (!preview || !selectedArchivePath) return;
  if (selectedIconPath.startsWith('blob:')) {
    preview.src = selectedIconPath;
    preview.hidden = false;
    if (empty) empty.textContent = 'Replacement preview';
    return;
  }
  try {
    const data = await GetArchiveIcon(selectedArchivePath, Number(pkg.value) || 0);
    preview.hidden = !data;
    preview.src = data || '';
    if (empty) empty.textContent = data ? '' : 'No embedded icon';
  } catch (e: any) {
    preview.hidden = true;
    preview.removeAttribute('src');
    if (empty) empty.textContent = `Icon unavailable: ${e}`;
  }
}

function setDroppedIcon(path: string) {
  if (!/\.(png|jpe?g)$/i.test(path)) {
    setActivity('Unsupported icon', 'Choose a PNG or JPEG image.', 'error');
    return;
  }
  if (!selectedArchivePath || !archivePackages.length) {
    setActivity('No package selected', 'Open an archive before dropping an image.', 'error');
    return;
  }

  selectedIconPath = path;
  (document.getElementById('edit-icon') as HTMLInputElement).value = path;
  const editButton = document.getElementById('edit-btn') as HTMLButtonElement;
  if (!editButton.disabled) editButton.click();
}

function setupTabs() {
  document.querySelectorAll<HTMLButtonElement>('.tab-btn').forEach(tab => {
    tab.addEventListener('click', () => {
      if (isRunning) return;
      const view = tab.dataset.tab === 'pack' ? 'tab-pack' : 'tab-info';
      showView(view);
    });
  });
  setupArchiveTools();
  showView('tab-pack');
}

function showView(viewId: string) {
  const view = document.getElementById(viewId);
  if (!view || isRunning) return;
  document.querySelectorAll<HTMLElement>('.tab-content').forEach(item => {
    item.classList.toggle('hidden', item !== view);
  });

  const archive = view.classList.contains('archive-tool');
  const packTab = document.querySelector<HTMLButtonElement>('[data-tab="pack"]');
  const archiveTab = document.querySelector<HTMLButtonElement>('[data-tab="info"]');
  packTab?.classList.toggle('active', !archive);
  packTab?.setAttribute('aria-current', archive ? 'false' : 'page');
  archiveTab?.classList.toggle('active', archive);
  archiveTab?.setAttribute('aria-current', archive ? 'page' : 'false');
  document.getElementById('archive-actions')?.classList.toggle('hidden', !archive);

  document.querySelectorAll<HTMLButtonElement>('.archive-action').forEach(button => {
    button.classList.toggle('active', archive && button.dataset.view === viewId);
  });

  const heading = view.querySelector('h2')?.textContent || (archive ? 'Container Info' : 'Pack a package');
  document.getElementById('workspace-title')!.textContent = archive ? heading : 'Pack a package';
  document.getElementById('workspace-crumb')!.textContent = archive ? 'ARCHIVE' : 'PACK';
  document.getElementById('main-content')?.scrollTo(0, 0);
  setActivity(archive ? heading : 'Pack a package', archive ? (selectedArchivePath || 'Open a NUP to inspect and edit its packages.') : 'Choose a source folder and output file to build a NUP.', 'ready');
}

function setupArchiveTools() {
  const host = document.getElementById('archive-actions');
  if (!host || host.childElementCount) return;
  document.querySelectorAll('.archive-tool').forEach(tool => {
    const heading = tool.querySelector('h2');
    if (!heading) return;
    const button = document.createElement('button');
    button.className = 'archive-action';
    button.type = 'button';
    button.textContent = heading.textContent || 'Archive action';
    button.dataset.view = (tool as HTMLElement).id;
    button.addEventListener('click', () => {
      showView((tool as HTMLElement).id);
    });
    host.appendChild(button);
  });
}

function setupPack() {
  const selectSrc = document.getElementById('pack-select-src') as HTMLButtonElement;
  const selectOut = document.getElementById('pack-select-out') as HTMLButtonElement;
  const packBtn = document.getElementById('pack-btn') as HTMLButtonElement;
  const srcInput = document.getElementById('pack-source') as HTMLInputElement;
  const outInput = document.getElementById('pack-output') as HTMLInputElement;
  const iconInput = document.getElementById('pack-icon') as HTMLInputElement;
  const selectIcon = document.getElementById('pack-select-icon') as HTMLButtonElement;
  const log = document.getElementById('pack-log') as HTMLPreElement;
  const progress = document.getElementById('pack-progress') as HTMLDivElement;
  const progressBar = document.getElementById('pack-progress-bar') as HTMLDivElement;

  log.style.display = 'none';

  selectSrc.addEventListener('click', async () => {
    if (isRunning) return;
    const dir = await SelectDirectory();
    if (dir) {
      srcInput.value = dir;
      try { await GetPackageInfo(dir + '/base'); } catch (e) {}
      try { await GetPackageInfo(dir); } catch (e) {}
    }
  });
  selectOut.addEventListener('click', async () => {
    if (isRunning) return;
    const file = await SaveFile();
    if (file) outInput.value = file;
  });
  selectIcon.addEventListener('click', async () => {
    if (isRunning) return;
    const file = await SelectIconFile();
    if (file) setPackIcon(file);
  });
  const packDrop = document.getElementById('pack-icon-drop');
  packDrop?.addEventListener('click', async event => {
    if ((event.target as HTMLElement).closest('button')) return;
    if (isRunning) return;
    try {
      const file = await SelectIconFile();
      if (file) setPackIcon(file);
    } catch (e: any) { setActivity('Icon selection failed', String(e), 'error'); }
  });
  OnFileDrop((_x, _y, paths) => {
    if (!packDrop?.classList.contains('wails-drop-target-active')) return;
    if (isRunning) return;
    if (paths.length !== 1 || !/\.(png|jpe?g)$/i.test(paths[0] || '')) {
      setActivity('Unsupported icon', 'Drop one PNG or JPEG image to use as the package icon.', 'error');
      return;
    }
    setPackIcon(paths[0]);
  }, true);
  packBtn.addEventListener('click', async () => {
    if (isRunning) {
      cancelRequested = true;
      packBtn.textContent = 'Cancelling...';
      packBtn.disabled = true;
      void CancelPack().catch((e: any) => {
        log.textContent = `Could not cancel: ${e}`;
      });
      return;
    }
    isRunning = true;
    cancelRequested = false;
    packBtn.disabled = false;
    packBtn.textContent = 'Cancel';
    const src = srcInput.value;
    const out = outInput.value;
    let total = 0;
    try { total = await CountFiles(src); } catch (e) { total = 0; }
    log.textContent = total > 0 ? `Packing source: ${src}\n${total} files found\n` : `Packing source: ${src}\n`;
    setActivity('Packing package', 'Preparing source and output…', 'running');
    progress.style.display = 'block';
    progressBar.style.width = '0%';
    progressBar.className = 'h-full bg-gradient-to-r from-purple-500 to-blue-500 transition-all duration-150';
    const mainEl = document.getElementById('main-content') as HTMLElement;
    let done = 0;
    let finishing = false;
    EventsOn('pack-progress', (data: any) => {
      const evDone = typeof data?.done === 'number' ? data.done : 0;
      const evTotal = typeof data?.total === 'number' ? data.total : 0;
      if (evTotal <= 0) return;
      done = evDone;
      if (done >= evTotal) {
        finishing = true;
        progressBar.className = 'h-full bg-gradient-to-r from-purple-400 to-blue-400 animate-pulse';
        progressBar.style.width = '100%';
        log.textContent = 'Finalizing: writing metadata...\n';
        mainEl.scrollTop = mainEl.scrollHeight;
        return;
      }
      const pct = (done / evTotal) * 100;
      progressBar.style.width = pct.toFixed(1) + '%';
      log.textContent = `Remaining: ${Math.max(0, evTotal - done)} files\n`;
      mainEl.scrollTop = mainEl.scrollHeight;
    });
    try {
      const opts: PackRequest = {
        level: parseInt((document.getElementById('pack-level') as HTMLInputElement).value) || 3,
        titleId: (document.getElementById('pack-titleid') as HTMLInputElement).value,
        displayVersion: (document.getElementById('pack-display') as HTMLInputElement).value,
        publisher: (document.getElementById('pack-publisher') as HTMLInputElement).value,
        name: (document.getElementById('pack-name') as HTMLInputElement).value,
        langNames: {},
        icon: iconInput.value,
        langIcons: {},
        keepListings: (document.getElementById('pack-keep') as HTMLInputElement).checked,
        quiet: false
      };
      if (cancelRequested) throw new Error('Cancelled');
      const res = await PackWithProgress(src, out, opts);
      EventsOff('pack-progress');
      if (finishing) {
        let text = 'Finalizing: ...';
        try { const sz = await GetFileSize(out); text = `Finalizing: ${sz.toFixed(2)} Gb`; } catch (e) {}
        progressBar.className = 'h-full bg-gradient-to-r from-purple-500 to-blue-500';
        progressBar.style.width = '100%';
        log.textContent = `${text}\n${res}\nDone!\n`;
      } else {
        progressBar.style.width = '100%';
        log.textContent = `Remaining: 0 files\n${res}\nDone!\n`;
      }
      isRunning = false;
      cancelRequested = false;
      packBtn.disabled = false;
      packBtn.textContent = 'Pack';
      setActivity('Pack complete', res);
      mainEl.scrollTop = mainEl.scrollHeight;
      setTimeout(() => { progress.style.display = 'none'; progressBar.style.width = '0%'; }, 500);
    } catch (e: any) {
      EventsOff('pack-progress');
      progressBar.className = 'h-full bg-gradient-to-r from-purple-500 to-blue-500';
      const wasCancelled = cancelRequested;
      if (wasCancelled) log.textContent = `Remaining: ${Math.max(0, total - done)} files\nCancelled.\n`;
      else log.textContent = `Remaining: ${Math.max(0, total - done)} files\nError: ${e}\n`;
      isRunning = false;
      cancelRequested = false;
      packBtn.disabled = false;
      packBtn.textContent = 'Pack';
      setActivity(wasCancelled ? 'Pack cancelled' : 'Pack failed', wasCancelled ? 'The pack operation was cancelled.' : String(e), wasCancelled ? 'ready' : 'error');
      setTimeout(() => { progress.style.display = 'none'; progressBar.style.width = '0%'; }, 500);
    }
  });
}

function setPackIcon(path: string) {
  (document.getElementById('pack-icon') as HTMLInputElement).value = path;
  const preview = document.getElementById('pack-icon-preview') as HTMLImageElement;
  const empty = document.getElementById('pack-icon-empty');
  preview.src = path;
  preview.hidden = false;
  if (empty) empty.textContent = 'Icon selected';
  setActivity('Package icon selected', path);
}

function setupInfo() {
  const input = document.getElementById('info-file') as HTMLInputElement;
  const btn = document.getElementById('info-btn') as HTMLButtonElement;
  const out = document.getElementById('info-output') as HTMLPreElement;
  document.getElementById('archive-package')?.addEventListener('change', () => { void refreshArchiveIcon(); });
  btn.addEventListener('click', async () => {
    try { out.textContent = await Info(selectedArchivePath || input.value); setActivity('Archive info', out.textContent); } catch (e: any) { out.textContent = 'Error: ' + e; setActivity('Info failed', String(e), 'error'); }
  });
  const drop = document.getElementById('archive-icon-drop') as HTMLElement;
  const setIconPath = (path: string) => {
    selectedIconPath = path;
    (document.getElementById('edit-icon') as HTMLInputElement).value = path;
    setActivity('Icon selected', 'Native image path selected. Click Update Metadata in Edit to apply it.');
  };
  document.getElementById('archive-icon-browse')?.addEventListener('click', async event => {
    event.stopPropagation();
    try {
      const path = await SelectIconFile();
      if (path) {
        setIconPath(path);
      }
    } catch (e: any) { setActivity('Icon selection failed', String(e), 'error'); }
  });
  drop?.addEventListener('click', event => {
    if ((event.target as HTMLElement).closest('button')) return;
    document.getElementById('archive-icon-browse')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
  });
  drop?.addEventListener('click', event => {
    if (event.target === drop || event.target === document.getElementById('archive-icon-empty')) {
      document.getElementById('archive-icon-browse')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    }
  });
  OnFileDrop((_x, _y, paths) => {
    drop?.classList.remove('drag-over');
    const path = paths[0];
    if (!path) return;
    if (paths.length !== 1) {
      setActivity('Choose one icon', 'Drop a single PNG or JPEG image.', 'error');
      return;
    }
    setDroppedIcon(path);
  }, true);
}

function setupList() {
  const select = document.getElementById('list-select') as HTMLButtonElement | null;
  const input = document.getElementById('list-file') as HTMLInputElement;
  const btn = document.getElementById('list-btn') as HTMLButtonElement;
  const out = document.getElementById('list-output') as HTMLPreElement;

  select?.addEventListener('click', async () => {
    const file = await SelectFile();
    if (file) await loadArchive(file);
  });
  btn.addEventListener('click', async () => {
    try { out.textContent = await List(selectedArchivePath || input.value); setActivity('Archive contents', out.textContent); } catch (e: any) { out.textContent = 'Error: ' + e; setActivity('List failed', String(e), 'error'); }
  });
}

function setupExtract() {
  const select = document.getElementById('extract-select') as HTMLButtonElement | null;
  const selectDest = document.getElementById('extract-select-dest') as HTMLButtonElement;
  const input = document.getElementById('extract-file') as HTMLInputElement;
  const dest = document.getElementById('extract-dest') as HTMLInputElement;
  const btn = document.getElementById('extract-btn') as HTMLButtonElement;
  const out = document.getElementById('extract-output') as HTMLPreElement;

  out.style.display = 'block';

  select?.addEventListener('click', async () => {
    if (isRunning) return;
    const file = await SelectFile();
    if (file) await loadArchive(file);
  });
  selectDest.addEventListener('click', async () => {
    if (isRunning) return;
    const dir = await SelectSaveDir();
    if (dir) dest.value = dir;
  });
  btn.addEventListener('click', async () => {
    if (isRunning) return;
    isRunning = true;
    btn.disabled = true;
    btn.textContent = 'Extracting…';
    out.textContent = 'Extracting…';
    setActivity('Extracting archive', 'The operation is running. No progress is reported by the backend.', 'running');
    const mainEl2 = document.getElementById('main-content') as HTMLElement;
    try {
      const pkg = (document.getElementById('archive-package') as HTMLSelectElement).value;
      const subtree = (document.getElementById('extract-subtree') as HTMLInputElement).value;
      const res = await Extract(selectedArchivePath || input.value, dest.value, pkg, subtree);
      out.textContent = `${res}\nDone!\n`;
      setActivity('Extract complete', res);
      isRunning = false;
      btn.disabled = false;
      btn.textContent = 'Extract';
      mainEl2.scrollTop = mainEl2.scrollHeight;
    } catch (e: any) {
      out.textContent = `Error: ${e}\n`;
      setActivity('Extract failed', String(e), 'error');
      isRunning = false;
      btn.disabled = false;
      btn.textContent = 'Extract';
    }
  });
}

function setupEdit() {
  const select = document.getElementById('edit-select') as HTMLButtonElement | null;
  const input = document.getElementById('edit-file') as HTMLInputElement;
  const btn = document.getElementById('edit-btn') as HTMLButtonElement;
  const out = document.getElementById('edit-output') as HTMLPreElement;
  btn.disabled = true;

  document.getElementById('edit-select-icon')?.addEventListener('click', async () => {
    try {
      const path = await SelectIconFile();
      if (path) {
        selectedIconPath = path;
        (document.getElementById('edit-icon') as HTMLInputElement).value = path;
        setActivity('Icon selected', 'Click Update Metadata to apply the selected icon.');
      }
    } catch (e: any) { setActivity('Icon selection failed', String(e), 'error'); }
  });

  select?.addEventListener('click', async () => {
    const file = await SelectFile();
    if (file) await loadArchive(file);
  });
  btn.addEventListener('click', async () => {
    try {
      const opts = {
        package: (document.getElementById('archive-package') as HTMLSelectElement).value,
        name: (document.getElementById('edit-name') as HTMLInputElement).value,
        publisher: (document.getElementById('edit-publisher') as HTMLInputElement).value,
        displayVersion: (document.getElementById('edit-display') as HTMLInputElement).value,
        icon: selectedIconPath.startsWith('blob:') ? (document.getElementById('edit-icon') as HTMLInputElement).value : selectedIconPath,
        langNames: {},
        langIcons: {},
        clearName: (document.getElementById('edit-clear-name') as HTMLInputElement).checked,
        clearPublisher: (document.getElementById('edit-clear-pub') as HTMLInputElement).checked,
        clearDisplay: (document.getElementById('edit-clear-disp') as HTMLInputElement).checked,
        clearIcon: (document.getElementById('edit-clear-icon') as HTMLInputElement).checked
      };
      const res = await Edit(selectedArchivePath || input.value, opts);
      out.textContent = res;
      setActivity('Metadata updated', res);
      selectedIconPath = '';
      await refreshArchiveIcon();
    } catch (e: any) { out.textContent = 'Error: ' + e; setActivity('Edit failed', String(e), 'error'); }
  });
}

function setupVerify() {
  const select = document.getElementById('verify-select') as HTMLButtonElement | null;
  const input = document.getElementById('verify-file') as HTMLInputElement;
  const btn = document.getElementById('verify-btn') as HTMLButtonElement;
  const out = document.getElementById('verify-output') as HTMLPreElement;

  select?.addEventListener('click', async () => {
    const file = await SelectFile();
    if (file) await loadArchive(file);
  });
  btn.addEventListener('click', async () => {
    try {
      const res = await Verify(selectedArchivePath || input.value);
      out.textContent = res;
      out.className = 'mt-4 p-4 bg-black/40 rounded-lg text-green-400 text-sm overflow-auto max-h-96 font-mono';
      setActivity('Verification complete', res);
    } catch (e: any) {
      out.textContent = e.toString();
      out.className = 'mt-4 p-4 bg-black/40 rounded-lg text-red-400 text-sm overflow-auto max-h-96 font-mono';
      setActivity('Verification failed', e.toString(), 'error');
    }
  });
}

window.addEventListener('DOMContentLoaded', () => {
  setupTabs();
  setupPack();
  document.getElementById('archive-open')?.addEventListener('click', async () => {
    try {
      const path = await SelectFile();
      if (path) {
        await loadArchive(path);
        document.querySelector<HTMLButtonElement>('[data-tab="info"]')?.click();
      }
    } catch (e: any) { setActivity('Open failed', String(e), 'error'); }
  });
  setupInfo();
  setupList();
  setupExtract();
  setupEdit();
  setupVerify();
});

window.addEventListener('beforeunload', () => OnFileDropOff());
