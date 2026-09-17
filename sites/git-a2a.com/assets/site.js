(() => {
  const embedded = document.getElementById('transcript-data');
  const transcript = embedded ? JSON.parse(embedded.textContent) : null;
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
    const label = button.querySelector('[aria-live]');
    const original = label.textContent;
    let restoreLabel;
    button.addEventListener('click', async () => {
      const source = button.dataset.copy === 'terminal'
        ? button.dataset.copyCommand || transcript.groups.map(group => group.command).join('\n')
        : document.querySelector(button.dataset.copy).textContent;
      await copyText(source);
      label.textContent = 'copied';
      window.clearTimeout(restoreLabel);
      restoreLabel = window.setTimeout(() => {
        label.textContent = original;
        restoreLabel = undefined;
      }, 1400);
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
      const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1
        : (index + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
      activate(tabs[next]);
      tabs[next].focus();
    });
  });

  const body = document.getElementById('terminal-body');
  const replay = document.getElementById('terminal-replay');
  if (!body || !replay || !transcript) return;
  const line = (kind, value) => {
    const row = document.createElement('div');
    row.className = `term-line ${kind}`;
    if (kind === 'command') {
      const prompt = document.createElement('span');
      prompt.className = 'prompt';
      prompt.textContent = '$ ';
      row.append(prompt, escapeText(value));
    } else {
      row.textContent = value;
    }
    body.append(row);
    body.scrollTop = body.scrollHeight;
    return row;
  };
  const caret = () => {
    const row = line('command', '');
    const mark = document.createElement('span');
    mark.className = 'caret';
    row.append(mark);
  };
  const finished = () => {
    body.replaceChildren();
    transcript.groups.forEach((group, index) => {
      line('command', group.command);
      outputLines(group).forEach(item => line(item.class, item.text));
      if (index < transcript.groups.length - 1) line('blank', '');
    });
    caret();
  };
  const play = () => {
    clearTimers();
    body.replaceChildren();
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      finished();
      return;
    }
    const { timing, groups } = transcript;
    const playGroup = groupIndex => {
      if (groupIndex >= groups.length) {
        caret();
        return;
      }
      const group = groups[groupIndex];
      const row = line('command', '');
      let commandIndex = 0;
      const outputs = outputLines(group);
      const playOutputs = outputIndex => {
        if (outputIndex < outputs.length) {
          line(outputs[outputIndex].class, outputs[outputIndex].text);
          later(() => playOutputs(outputIndex + 1), timing.betweenOutput);
          return;
        }
        if (groupIndex < groups.length - 1) line('blank', '');
        later(() => playGroup(groupIndex + 1), timing.betweenGroups);
      };
      const typeCommand = () => {
        if (commandIndex < group.command.length) {
          row.append(escapeText(group.command[commandIndex++]));
          later(typeCommand, timing.character);
          return;
        }
        later(() => playOutputs(0), timing.afterCommand);
      };
      typeCommand();
    };
    later(() => playGroup(0), timing.initial);
  };
  replay.addEventListener('click', play);
  play();
})();
