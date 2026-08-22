(async () => {
  const embedded = document.getElementById('transcript-data');
  if (embedded) {
    try {
      const response = await fetch(new URL('transcript.json', document.currentScript.src));
      if (!response.ok) throw new Error(String(response.status));
      window.gitA2ATranscript = await response.json();
    } catch (_) {
      window.gitA2ATranscript = JSON.parse(embedded.textContent);
    }
  }
  const timers = new Set();
  const later = (fn, delay) => {
    const id = window.setTimeout(() => { timers.delete(id); fn(); }, delay);
    timers.add(id);
  };
  const clearTimers = () => { timers.forEach(window.clearTimeout); timers.clear(); };
  const escapeText = text => document.createTextNode(text);
  const outputLines = group => group.render.flatMap(segment => {
    const values = group[segment.stream].split('\n');
    if (values.at(-1) === '') values.pop();
    return values.map((text, index) => ({ text, class: segment.classes[index] }));
  });
  const copyText = async text => {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch (_) {
      const fallback = document.createElement('textarea');
      fallback.value = text;
      fallback.setAttribute('readonly', '');
      fallback.style.cssText = 'position:fixed;opacity:0;pointer-events:none';
      document.body.append(fallback);
      fallback.select();
      document.execCommand('copy');
      fallback.remove();
    }
  };

  document.querySelectorAll('[data-copy]').forEach(button => {
    button.addEventListener('click', async () => {
      const source = button.dataset.copy === 'terminal'
        ? window.gitA2ATranscript.groups.map(group => group.command).join('\n')
        : document.querySelector(button.dataset.copy).textContent;
      await copyText(source);
      const label = button.querySelector('[aria-live]');
      label.textContent = 'copied';
      later(() => { label.textContent = 'copy'; }, 1400);
    });
  });

  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const activate = tab => {
    tabs.forEach(item => {
      const selected = item === tab;
      item.setAttribute('aria-selected', String(selected));
      item.tabIndex = selected ? 0 : -1;
      document.getElementById(item.getAttribute('aria-controls')).hidden = !selected;
    });
  };
  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => activate(tab));
    tab.addEventListener('keydown', event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault();
      let next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1
        : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
      activate(tabs[next]);
      tabs[next].focus();
    });
  });

  const body = document.getElementById('terminal-body');
  const replay = document.getElementById('terminal-replay');
  if (!body || !replay) return;
  const line = (kind, text, comment = '') => {
    const row = document.createElement('div');
    row.className = `term-line ${kind}`;
    if (kind === 'command') {
      const prompt = document.createElement('span');
      prompt.className = 'prompt'; prompt.textContent = '$ ';
      row.append(prompt, escapeText(text));
      if (comment) { const note = document.createElement('span'); note.className = 'term-comment'; note.textContent = `  ${comment}`; row.append(note); }
    } else row.textContent = text;
    body.append(row);
    body.scrollTop = body.scrollHeight;
    return row;
  };
  const caret = () => { const row = line('command', ''); const mark = document.createElement('span'); mark.className = 'caret'; row.append(mark); };
  const finished = () => {
    body.replaceChildren();
    window.gitA2ATranscript.groups.forEach((group, index) => {
      line('command', group.command, group.comment);
      outputLines(group).forEach(item => line(item.class, item.text));
      if (index < window.gitA2ATranscript.groups.length - 1) line('blank', '');
    });
    caret();
  };
  const play = () => {
    clearTimers(); body.replaceChildren();
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) { finished(); return; }
    const timing = window.gitA2ATranscript.timing;
    let wait = timing.initial;
    window.gitA2ATranscript.groups.forEach((group, groupIndex) => {
      later(() => {
        const row = line('command', '', group.comment);
        const note = row.querySelector('.term-comment');
        if (note) note.remove();
        let index = 0;
        const type = () => {
          if (index < group.command.length) { row.append(escapeText(group.command[index++])); later(type, timing.character); return; }
          if (group.comment) { const span = document.createElement('span'); span.className = 'term-comment'; span.textContent = `  ${group.comment}`; row.append(span); }
          outputLines(group).forEach((item, outputIndex) => later(() => line(item.class, item.text), timing.afterCommand + outputIndex * timing.betweenOutput));
        };
        type();
      }, wait);
      wait += group.command.length * timing.character + timing.afterCommand + outputLines(group).length * timing.betweenOutput;
      if (groupIndex < window.gitA2ATranscript.groups.length - 1) later(() => line('blank', ''), wait);
      wait += timing.betweenGroups;
    });
    later(caret, wait);
  };
  replay.addEventListener('click', play);
  play();
})();
