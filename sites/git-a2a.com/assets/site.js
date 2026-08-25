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
  const exchangeLine = (exchange, input = exchange.input) => {
    const row = document.createElement('div');
    row.className = 'term-line exchange';
    row.dataset.defaultAccepted = String(Boolean(exchange.defaultAccepted));
    const prompt = document.createElement('span');
    prompt.className = 'exchange-prompt'; prompt.textContent = exchange.prompt;
    const value = document.createElement('span');
    value.className = 'exchange-input'; value.textContent = input;
    row.append(prompt);
    if (exchange.input) row.append(escapeText(' '));
    row.append(value); body.append(row); body.scrollTop = body.scrollHeight;
    return value;
  };
  const finished = () => {
    body.replaceChildren();
    window.gitA2ATranscript.groups.forEach((group, index) => {
      line('command', group.command, group.comment);
      (group.exchanges || []).forEach(exchange => exchangeLine(exchange));
      outputLines(group).forEach(item => line(item.class, item.text));
      if (index < window.gitA2ATranscript.groups.length - 1) line('blank', '');
    });
    caret();
  };
  const play = () => {
    clearTimers(); body.replaceChildren();
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) { finished(); return; }
    const timing = window.gitA2ATranscript.timing;
    const groups = window.gitA2ATranscript.groups;
    const playGroup = groupIndex => {
      if (groupIndex >= groups.length) { caret(); return; }
      const group = groups[groupIndex];
      const row = line('command', '');
      let commandIndex = 0;
      const playOutputs = outputIndex => {
        const output = outputLines(group);
        if (outputIndex < output.length) {
          line(output[outputIndex].class, output[outputIndex].text);
          later(() => playOutputs(outputIndex + 1), timing.betweenOutput);
          return;
        }
        if (groupIndex < groups.length - 1) line('blank', '');
        later(() => playGroup(groupIndex + 1), timing.betweenGroups);
      };
      const playExchange = exchangeIndex => {
        const exchanges = group.exchanges || [];
        if (exchangeIndex >= exchanges.length) { playOutputs(0); return; }
        const exchange = exchanges[exchangeIndex];
        const input = exchangeLine(exchange, '');
        let inputIndex = 0;
        const typeInput = () => {
          if (inputIndex < exchange.input.length) {
            input.append(escapeText(exchange.input[inputIndex++]));
            later(typeInput, timing.inputCharacter);
            return;
          }
          later(() => playExchange(exchangeIndex + 1), timing.betweenOutput);
        };
        typeInput();
      };
      const typeCommand = () => {
        if (commandIndex < group.command.length) {
          row.append(escapeText(group.command[commandIndex++]));
          later(typeCommand, timing.character);
          return;
        }
        if (group.comment) {
          const note = document.createElement('span'); note.className = 'term-comment';
          note.textContent = `  ${group.comment}`; row.append(note);
        }
        later(() => playExchange(0), timing.afterCommand);
      };
      typeCommand();
    };
    later(() => playGroup(0), timing.initial);
  };
  replay.addEventListener('click', play);
  play();
})();
