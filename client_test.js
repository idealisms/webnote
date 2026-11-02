// Unit tests for the parts of the client that don't need a page. Run with
// `node --test` (js_test.go does, as part of `go test`). browsertest.py
// covers the rest in real browsers.
//
// Each test imports webnote.js afresh (a new query string makes a new
// module), with stand-ins for the few browser globals it touches.
// objects.js has no state, so the tests share it.

'use strict';

process.env.TZ = 'Asia/Tokyo';  // Not the server's US/Eastern.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const {pathToFileURL} = require('node:url');
const vm = require('node:vm');

// strings.js is a classic script that defines a global.
vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'strings.js.en'), 'utf8'));

let loads = 0;
async function load() {
  const elements = {};
  const window = {innerWidth: 1280, innerHeight: 800};
  const document = {getElementById: id => elements[id] || (elements[id] = {})};
  Object.assign(globalThis, {window, document});
  Object.defineProperty(globalThis, 'navigator',
                        {value: {userAgent: 'node'}, configurable: true, writable: true});
  const url = file => pathToFileURL(path.join(__dirname, file)).href;
  const objects = await import(url('objects.js'));
  const webnote = await import(url('webnote.js') + '?n=' + ++loads + '&v=test');
  return {...objects, ...webnote, window, document};
}

// markdown.js has no state and no DOM; decode stands in for the page's
// decodeCharRefs().
const markdown = import(pathToFileURL(path.join(__dirname, 'markdown.js')).href);
const NAMED = {amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: '\u00a0'};
const decode = s => s.replace(/&(?:#(\d+)|#x([0-9a-f]+)|(\w+));/gi, (ref, dec, hex, name) =>
  dec ? String.fromCodePoint(Number(dec)) : hex ? String.fromCodePoint(parseInt(hex, 16))
      : NAMED[name] || ref);

test('getHTML renders the note with markdown.js', async () => {
  const {Note} = await load();
  const html = text => Note.prototype.getHTML.call({text});
  assert.equal(html('**a**\n<b>b</b>'), '<strong>a</strong><br /><strong>b</strong>');
});

test('markdown: lines, the wiki syntax and blocks', async () => {
  const {renderNote} = await markdown;
  const html = text => renderNote(text, decode);
  assert.equal(html(''), '');
  assert.equal(html('a\nb'), 'a<br />b');
  assert.equal(html('a\n\nb'), 'a<br /><br />b', 'blank lines show');
  assert.equal(html('a\\\nb'), 'ab', 'a backslash joins lines');
  assert.equal(html('x ! y'), 'x ! y');
  // Wiki headings: more !s are bigger, up to h1.
  assert.equal(html('!h3\n!!h2\n!!!h1\n!!!!h1'), '<h3>h3</h3><h2>h2</h2><h1>h1</h1><h1>h1</h1>');
  assert.equal(html('# one\n### three ###\n#tag\n#'), '<h1>one</h1><h3>three</h3>#tag<br />#');
  // Before and after a block, a blank line still shows.
  assert.equal(html('a\n# h\nb'), 'a<br /><h1>h</h1>b');
  assert.equal(html('a\n\n# h\n\nb'), 'a<br /><br /><h1>h</h1><br />b');
  assert.equal(html('- a\n- b\n  - c\n    d\n* e'),
               '<ul><li>a</li><li>b<ul><li>c<br />d</li></ul></li><li>e</li></ul>');
  assert.equal(html('3. x\n4. y\n- z'), '<ol start="3"><li>x</li><li>y</li></ol><ul><li>z</li></ul>');
  assert.equal(html('- a\n\n- b'), '<ul><li>a</li></ul><br /><ul><li>b</li></ul>',
               'a blank line ends a list');
  assert.equal(html('-5 degrees\n1.5 cups'), '-5 degrees<br />1.5 cups');
  assert.equal(html('> q\n>\n> > qq\nafter'),
               '<blockquote>q<br /><br /><blockquote>qq</blockquote></blockquote>after');
  assert.equal(html('>>> x\n>next'), '&gt;&gt;&gt; x<br />&gt;next', 'arrows aren\'t quotes');
  assert.equal(html('---\n* * *'), '<hr /><hr />');
  assert.equal(html('```\n<b>x</b> **y**\n```\nz'),
               '<pre><code><strong>x</strong> **y**</code></pre>z'.replace('<strong>x</strong>', '**x**'),
               'code blocks are text (legacy <b> still converts first)');
  assert.equal(html('```\nunclosed'), '<pre><code>unclosed</code></pre>');
});

test('markdown: inline', async () => {
  const {renderNote} = await markdown;
  const html = text => renderNote(text, decode);
  assert.equal(html('**b** __b__ *i* _i_ ***bi*** ~~s~~'),
               '<strong>b</strong> <strong>b</strong> <em>i</em> <em>i</em> '
               + '<strong><em>bi</em></strong> <del>s</del>');
  assert.equal(html('*a **b** c*'), '<em>a <strong>b</strong> c</em>');
  assert.equal(html('snake_case_name 2 * 3 * 4 ** x'), 'snake_case_name 2 * 3 * 4 ** x');
  assert.equal(html('**unclosed and * lone'), '**unclosed and * lone');
  assert.equal(html('`a*b*` ``x`y``'), '<code>a*b*</code> <code>x`y</code>');
  assert.equal(html('\\*not\\* \\[x\\](y) C:\\dir'), '*not* [x](y) C:\\dir');
  assert.equal(html('<x> & "q" \'s\''), '&lt;x&gt; &amp; &quot;q&quot; &#39;s&#39;');
});

test('markdown: links and images', async () => {
  const {renderNote} = await markdown;
  const html = text => renderNote(text, decode);
  const link = (url, text = url) => '<a href="' + url + '">' + text + '</a>';
  assert.equal(html('http://e.com/start'), link('http://e.com/start'));
  assert.equal(html('see https://e.com/s. ok'), 'see ' + link('https://e.com/s') + '. ok');
  assert.equal(html('a\tftp://e.com/f'), 'a\t' + link('ftp://e.com/f'));
  assert.equal(html('line\nhttp://e.com/x'), 'line<br />' + link('http://e.com/x'));
  assert.equal(html('http://e.com/a?x=1&y=2 z'), link('http://e.com/a?x=1&amp;y=2') + ' z');
  assert.equal(html('nohttp://e.com mailto:x@e.com'), 'nohttp://e.com mailto:x@e.com');
  // Quirk, pinned: a fragment swallows a trailing '.'.
  assert.equal(html('http://e.com/a#frag. x'), link('http://e.com/a#frag.') + ' x');
  assert.equal(html('[t **b**](https://e.com/(a)) [m](mailto:x@e.com "title")'),
               link('https://e.com/(a)', 't <strong>b</strong>') + ' ' + link('mailto:x@e.com', 'm'));
  assert.equal(html('<https://e.com/x> [https://e.com/in](https://e.com/out)'),
               link('https://e.com/x') + ' ' + link('https://e.com/out', 'https://e.com/in'),
               'no links in links');
  assert.equal(html('![a "b"](https://e.com/i.png)'), '<img src="https://e.com/i.png" alt="a &quot;b&quot;" />');
  assert.equal(html('[![i](https://e.com/i.png)](https://e.com/)'),
               link('https://e.com/', '<img src="https://e.com/i.png" alt="i" />'));
  // Only web addresses: anything else stays text.
  for (const bad of ['[x](javascript:alert(1))', '[x](JaVaScRiPt:alert(1))', '[x](/relative)',
                     '[x](data:text/html,hi)', '<javascript:alert(1)>',
                     '[x](java&#115;cript:alert(1))']) {
    assert.ok(!/<a |<img /.test(html(bad)), bad + ' -> ' + html(bad));
  }
  assert.equal(html('![x](ftp://e.com/i.png)'), '!' + link('ftp://e.com/i.png', 'x'),
               'images need http(s), so this is a link');
  assert.equal(html('[a ]b](https://e.com) [\\[1](https://e.com)'),
               '[a ]b](https://e.com) ' + link('https://e.com', '[1'));
  // An image at the start of a line isn't a wiki heading.
  assert.equal(html('![i](https://e.com/i.png) x'), '<img src="https://e.com/i.png" alt="i" /> x');
});

test('markdown: the HTML notes could hold before', async () => {
  const {legacyToMarkdown, renderNote} = await markdown;
  const md = text => legacyToMarkdown(text, decode);
  assert.equal(md('<b>b</b> <STRONG>s</STRONG> <i>i</i> <em> e </em> <u>u</u> <s>x</s> <code>c</code>'),
               '**b** **s** *i*  *e*  u ~~x~~ `c`');
  assert.equal(md('a<br>b<BR/>c<br />d'), 'a\nb\nc\nd');
  assert.equal(md('<a href="https://e.com/a b">t [1]</a> <a href=http://e.com>http://e.com</a> <a href="https://e.com/"></a>'),
               '[t [1]](https://e.com/a%20b) <http://e.com> <https://e.com/>');
  assert.equal(md('<a href="javascript:alert(1)">t</a> <a href="/rel" onclick="x()">r</a>'), 't r');
  assert.equal(md('<a href="https://e.com"><img src="https://e.com/i.png" alt="i"></a>'),
               '[![i](https://e.com/i.png)](https://e.com)');
  assert.equal(md('<img src="https://e.com/i.png" onerror="alert(1)"> <img src=x onerror=alert(1)>'),
               '![](https://e.com/i.png) ');
  assert.equal(md('<p>one</p><p>two</p><div>three</div>'), 'one\ntwo\nthree\n');
  assert.equal(md('<h1>big</h1><ul><li>a<li>b</ul><hr>end'), '# big\n- a\n- b\n---\nend');
  assert.equal(md('x<script>alert("<b>")</script>y<style>p{}</style>z<!-- c -->!'), 'xyz!');
  assert.equal(md('<html><head><title>t</title></head><body><font color=red>hi</font></body></html>'),
               'hi\n');
  assert.equal(md('&nbsp;caf&#233; &lt;b&gt; <span title="a>b">x</span>'), '\u00a0café <b> x');
  // Text that only looks like HTML stays.
  assert.equal(md('<customer> <brand name> a < b > c <http://e.com> <o:p></o:p>'),
               '<customer> <brand name> a < b > c <http://e.com> ');
  assert.equal(md('unclosed <b>bold <a href="https://e.com">link <script>x'),
               'unclosed <b>bold <a href="https://e.com">link <script>x');
  assert.equal(md('<b>a</b'), '<b>a</b', 'an incomplete tag is text');
  assert.equal(md('<a href="https://e.com">x] [y \\</a>'), '[x\\] \\[y \\\\](https://e.com)');
  assert.equal(renderNote('<a href="https://e.com">x] [y \\</a>', decode),
               '<a href="https://e.com">x] [y \\</a>');
  assert.equal(md('<a href="https://e.com/>t</a>'), '<a href="https://e.com/>t',
               'an unclosed quote makes it text (a stray closing tag goes)');
  assert.equal(md('<b>a\nb</b>'), 'a\nb', 'no emphasis across lines');
  // Whatever it was, the result renders safely.
  for (const evil of ['<img src=x onerror=alert(1)>', '<svg onload=alert(1)>', '<script>alert(1)',
                      '<a href="javascript:alert(1)">x</a>', '<iframe src="https://e.com">',
                      '&lt;script&gt;alert(1)&lt;/script&gt;', '<meta http-equiv=refresh content="0;url=https://e.com">',
                      '<scr<script>ipt>alert(1)</script>', '<<b>script>x</b>']) {
    const out = renderNote(evil, decode);
    assert.ok(!/<(?!\/?(?:strong|em|del|code|pre|br|a href="https?:|h[1-6]|ul|ol|li|blockquote|hr|img src="https?:))/i.test(out),
              evil + ' -> ' + out);
  }
});

test('markdown: large and nested notes stay fast', async () => {
  const {renderNote} = await markdown;
  for (const text of ['*a '.repeat(20000), '[a '.repeat(20000) + ']', '`'.repeat(20000),
                      '<b>'.repeat(20000), '<a href="https://e.com">'.repeat(5000), '> '.repeat(5000) + 'x',
                      '- '.repeat(5000) + 'x', ('  '.repeat(200) + '- x\n').repeat(200),
                      '**'.repeat(20000) + 'x', '_a '.repeat(20000) + '_', '<!--'.repeat(10000),
                      '*'.repeat(30) + 'x' + '*'.repeat(30), 'http://e.com/' + 'a'.repeat(100000)]) {
    const start = Date.now();
    renderNote(text, decode);
    assert.ok(Date.now() - start < 1000, text.slice(0, 20) + '...: ' + (Date.now() - start) + ' ms');
  }
});

test('charRefs stores characters 160-255 as references', async () => {
  const {charRefs} = await load();
  assert.equal(charRefs('café  ÿ Ā 日 \u{1f600} &amp; <b>'),
               'caf&#233; &#160;&#255; Ā 日 \u{1f600} &amp; <b>');
});

test('myescape', async () => {
  const {myescape} = await load();
  assert.equal(myescape('aZ09_'), 'aZ09_');
  assert.equal(myescape('my-notes.txt'), 'my%2Dnotes%2Etxt');
  assert.equal(myescape('Bogotá café'), 'Bogot%E1%20caf%E9');
  // %u escapes have 4 digits, and an astral character is two of them.
  assert.equal(myescape('ā י日\u{1f600}'), '%u0101%20%u05D9%u65E5%uD83D%uDE00');
  assert.equal(unescape(myescape('ā י日\u{1f600}')), 'ā י日\u{1f600}');
});

test('Color parses and prints', async () => {
  const {Color} = await load();
  for (const value of ['rgb(255, 16, 0)', 'ff1000', '#ff1000']) {
    assert.equal(new Color(value).toString(), '#ff1000', value);
  }
});

test('Color.hsvadj darkens each note color for hover', async () => {
  const {Color, bgColors} = await load();
  const hover = Array.from(bgColors, c => new Color(c).hsvadj(0, 0, -0.1).toString());
  assert.deepEqual(hover, ['#e5e52b', '#809ae5', '#72e564', '#e55656',
                           '#e5a239', '#e560d7', '#e5e5e5', '#969696']);
});

test('Point', async () => {
  const {Point} = await load();
  const p = new Point(3, 4);
  assert.equal(p.add(new Point(1, 2)).toString(), '(4, 6)');
  assert.equal(p.sub(new Point(1, 2)).toString(), '(2, 2)');
  assert.equal(p.div(2).toString(), '(1.5, 2)');
  assert.ok(p.copy().equals(p) && p.copy() !== p);
  assert.equal(new Point().toString(), '(0, 0)');
});

test('MyDate rounds to the second and shows local time', async () => {
  const {MyDate} = await load();
  const d = new MyDate('1792951601500001');
  assert.equal(d.dateobj.getTime(), 1792951602000);
  assert.match(String(new MyDate('0')), /1970.*9:00:00/, 'Tokyo is UTC+9');
});

test('History: undo, redo and the toolbar', async () => {
  const ctx = await load();
  const history = ctx.History;
  const log = [];
  const action = title => ({
    title,
    undo: () => log.push('undo ' + title),
    redo: () => log.push('redo ' + title),
  });
  const el = id => ctx.document.getElementById(id);

  history.updateTitles();
  assert.equal(el('undoImg').className, 'controlsDisabled');
  assert.equal(el('saveImg').className, 'controlsDisabled');

  history.add(action('a'));
  history.add(action('b'));
  assert.equal(el('undoImg').title, 'b (2 actions)');
  assert.equal(el('saveImg').className, 'controls', 'changes enable save');
  history.undo();
  assert.equal(el('undoImg').title, 'a (1 action)');
  assert.equal(el('redoImg').title, 'b (1 action)');
  history.redo();
  history.undo();
  history.add(action('c'));
  assert.equal(el('redoImg').className, 'controlsDisabled', 'add clears redo');
  history.redo();
  history.undo();
  history.undo();
  history.undo();
  assert.deepEqual(log, ['undo b', 'redo b', 'undo b', 'undo c', 'undo a']);
  assert.equal(el('saveImg').className, 'controlsDisabled', 'undoing everything disables save');
});

test('History keeps the last 40 actions', async () => {
  const ctx = await load();
  const history = ctx.History;
  for (let i = 0; i < 45; i++) {
    history.add({title: String(i), undo() {}, redo() {}});
  }
  assert.equal(history.undoStack.length, 40);
  assert.equal(history.undoStack[0].title, '5');
});

test('NotePos cascades new notes and wraps at the window edge', async () => {
  const ctx = await load();
  const pos = ctx.NotePos;
  assert.equal(pos.nextPos(250, 150).toString(), '(170, 40)');
  assert.equal(pos.nextPos(250, 150).toString(), '(190, 60)');
  let p;
  for (let i = 0; i < 40; i++) {
    p = pos.nextPos(250, 150);
  }
  assert.ok(p.y + 150 <= 800, 'stays in the window');
  ctx.window.innerHeight = 100;
  pos.nextPos(250, 150);
  assert.equal(pos.nextPos(250, 150).toString(), '(40, 40)');
});

// The poll for saves from other clients, with timers and getrecent.py stubbed.
async function loadPolling() {
  const ctx = await load();
  const timers = new Map();
  let nextTimer = 1;
  ctx.window.setTimeout = (f, ms) => {
    timers.set(nextTimer, f);
    return nextTimer++;
  };
  ctx.window.clearTimeout = id => timers.delete(id);
  ctx.document.visibilityState = 'visible';
  const requests = [];  // resolve or reject each to answer it
  globalThis.fetch = url => new Promise((resolve, reject) => requests.push({url, resolve, reject}));
  const ws = ctx.workspace;
  ws.name = 'ws';
  ws.loadedTime = 100;
  ws.clientVersion = 1;
  // The dialogs: showCollision() builds DOM, so it just opens its dialog.
  const collision = ctx.document.getElementById('collisionDialog');
  const conflict = ctx.document.getElementById('conflictDialog');
  ws.showCollision = () => { collision.open = true; };
  // client is the X-Webnote-Client header (the server's clientVersion).
  const answer = async (status, text, client = '1') => {
    const headers = {get: name => 'X-Webnote-Client' == name ? client : null};
    requests.shift().resolve({status, headers, text: async () => text});
    await new Promise(setImmediate);
  };
  const fire = () => {
    assert.equal(timers.size, 1, 'one pending check');
    const [[id, f]] = timers;
    timers.delete(id);
    f();
  };
  return {ctx, ws, timers, requests, answer, fire, collision, conflict};
}

test('checkUpdated keeps one check going, through failures', async () => {
  const {ctx, ws, timers, requests, answer, fire, collision} = await loadPolling();
  ws.schedulePoll();
  ws.schedulePoll();
  fire();
  assert.equal(requests.length, 1);
  assert.match(requests[0].url, /^getrecent\.py\?name=ws&epoch=1&client=1$/);
  ws.checkUpdated();
  assert.equal(requests.length, 1, 'no second check while one is in flight');
  await answer(500, 'error');
  fire();
  requests.shift().reject(new Error('network'));
  await new Promise(setImmediate);
  fire();
  await answer(200, '100');
  assert.equal(timers.size, 1, 'no change: check again later');
  assert.ok(!collision.open);

  // A hidden tab skips the check and leaves no timer, for visibilitychange.
  ctx.document.visibilityState = 'hidden';
  fire();
  assert.equal(requests.length, 0);
  assert.equal(timers.size, 0);
  assert.equal(ws.pollTimer, 0);
});

test('checkUpdated stops while the warning is shown', async () => {
  const {ws, timers, requests, answer, fire, collision, conflict} = await loadPolling();
  ws.checkUpdated();
  await answer(200, '200');
  assert.ok(collision.open, 'a newer save shows the warning');
  assert.equal(timers.size, 0);
  ws.checkUpdated();
  assert.equal(requests.length, 0, 'no checks while it is shown');

  // Closing it checks again, and the same save doesn't warn twice.
  collision.open = false;
  ws.schedulePoll();
  fire();
  await answer(200, '200');
  assert.ok(!collision.open);
  fire();
  await answer(200, '300');
  assert.ok(collision.open, 'a newer one does');

  // A conflict warning stops checks too.
  collision.open = false;
  conflict.open = true;
  ws.checkUpdated();
  assert.equal(requests.length, 0);
});

test('save ignores clicks while a save is in flight', async () => {
  const {ctx, ws, requests, answer} = await loadPolling();
  ctx.document.getElementById('saveImg').className = 'controls';
  ctx.document.getElementById('saveImg').dataset = {};
  ws.editOff = () => {};
  ws.toString = () => '<workspace/>';
  ws.save();
  ws.save();
  assert.equal(requests.length, 1, 'one save until the first answers');
  assert.match(requests[0].url, /^save\.py\?epoch=1&loaded=100&client=1$/);
  const alerts = [];
  globalThis.alert = message => alerts.push(message);
  await answer(500, 'error');
  assert.equal(alerts.length, 1);

  // After an answer, saving works again.
  ws.save();
  assert.equal(requests.length, 1);
  assert.match(requests[0].url, /loaded=100&client=1$/);
});

test('a newer client version asks to reload, and reloads only when hidden and safe', async () => {
  const {ctx, ws, requests, answer, fire, collision} = await loadPolling();
  let updated = 0, reloads = 0;
  ws.showUpdated = () => { updated++; };
  globalThis.location = {reload: () => { reloads++; }};
  ctx.document.getElementById('saveImg').dataset = {};

  // The same version, or none (an error page from the front end), is fine.
  ws.checkUpdated();
  await answer(200, '100');
  fire();
  await answer(500, 'error', null);
  assert.ok(!ws.needsReload);
  assert.equal(updated, 0);

  // A newer one shows the banner once, even on an error, and keeps polling.
  fire();
  await answer(500, 'error', '2');
  assert.ok(ws.needsReload);
  assert.equal(updated, 1);
  fire();
  await answer(200, '100', '2');
  assert.equal(updated, 1);
  assert.ok(!collision.open);
  assert.equal(reloads, 0, 'a visible page never reloads by itself');

  // Hidden: only once nothing would be lost.
  ctx.document.visibilityState = 'hidden';
  for (const [state, undo] of [
    [() => { ws.changed = true; }, () => { ws.changed = false; }],
    [() => { ws.edit = {}; }, () => { ws.edit = ''; }],
    [() => { ws.mouse.selObj = {}; }, () => { delete ws.mouse.selObj; }],
    [() => { get('saveImg').dataset.state = 'saving'; }, () => { get('saveImg').dataset.state = ''; }],
  ]) {
    state();
    ws.reloadIfHidden();
    assert.equal(reloads, 0);
    undo();
  }
  ws.reloadIfHidden();
  assert.equal(reloads, 1);

  function get(id) { return ctx.document.getElementById(id); }
});

test('a save or a late answer can find the page out of date while hidden', async () => {
  const {ctx, ws, requests, answer} = await loadPolling();
  let reloads = 0;
  ws.showUpdated = () => {};
  globalThis.location = {reload: () => { reloads++; }};
  const saveImg = ctx.document.getElementById('saveImg');
  saveImg.className = 'controls';
  saveImg.dataset = {};
  ws.editOff = () => {};
  ws.toString = () => '<workspace/>';
  ws.setSaveState = state => { saveImg.dataset.state = state; };
  ws.changed = true;
  ws.save();
  globalThis.alert = () => {};
  await answer(500, 'error', '2');  // saveCallback needs DOMParser
  assert.ok(ws.needsReload, 'a save response counts, whatever its status');
  assert.equal(reloads, 0, 'visible');

  // With the changes saved after all, a late poll answer reloads.
  ws.changed = false;
  ws.needsReload = false;
  ws.checkUpdated();
  ctx.document.visibilityState = 'hidden';
  await answer(200, '200', '2');
  assert.equal(reloads, 1, 'the answer came after the tab was hidden');
});

test('reportErrors sends each error once, at most 5, without the page URL', async () => {
  const {reportErrors} = await load();
  const listeners = {};
  const page = 'https://e.com/webnote/my%20notes';
  const win = {
    location: {href: page + '#x', origin: 'https://e.com'},
    addEventListener: (type, f) => { listeners[type] = f; },
  };
  const beacons = [];
  navigator.sendBeacon = (url, body) => beacons.push({url, report: JSON.parse(body)});
  reportErrors(win);

  const error = new TypeError('x is undefined');
  error.stack = 'TypeError: x is undefined\n    at f (' + page + ':3:9)';
  listeners.error({error, message: 'Uncaught TypeError: x is undefined',
                   filename: page, lineno: 3, colno: 9});
  listeners.error({error, message: 'Uncaught TypeError: x is undefined',
                   filename: page, lineno: 3, colno: 9});
  assert.equal(beacons.length, 1, 'the same message is sent once');
  assert.deepEqual(beacons[0], {url: 'jserror', report: {
    message: 'TypeError: x is undefined', source: 'page', line: 3, col: 9,
    stack: 'TypeError: x is undefined\n    at f (page:3:9)', version: 'test'}});

  listeners.error({error: null, message: 'Script error.', filename: '', lineno: 0, colno: 0});
  listeners.error({error: new Error('ext'), message: 'ext',
                   filename: 'chrome-extension://abc/x.js', lineno: 1, colno: 1});
  assert.equal(beacons.length, 1, 'errors from other origins are dropped');

  listeners.unhandledrejection({reason: 'rejected'});
  assert.equal(beacons[1].report.message, 'rejected');
  assert.equal(beacons[1].report.stack, '');
  for (let i = 0; i < 10; ++i)
    listeners.unhandledrejection({reason: new Error('e' + i)});
  assert.equal(beacons.length, 5, 'at most 5 per page');
});

test('docKeyDown: keys with no note on top are ignored', async () => {
  const {workspace, docKeyDown} = await load();
  assert.equal(workspace.topId, '');
  const key = (keyCode, altKey = false) =>
    docKeyDown({keyCode, altKey, preventDefault() {}});
  key(65);                // forwarded to the note
  key(79, true);          // Alt+O edits it
  workspace.edit = true;
  key(65);                // forwarded to the note
  key(68, true);          // Alt+D deletes it
});

test('recent workspaces: newest first, at most 8, and storage failures are ignored', async () => {
  const {recentWorkspaces, rememberWorkspace, forgetWorkspace} = await load();
  const store = {};
  globalThis.localStorage = {
    getItem: k => (k in store ? store[k] : null),
    setItem: (k, v) => { store[k] = String(v); },
  };
  assert.deepEqual(recentWorkspaces(), []);
  rememberWorkspace('a');
  rememberWorkspace('Bogotá café');
  rememberWorkspace('a');
  rememberWorkspace('');
  assert.deepEqual(recentWorkspaces(), ['a', 'Bogotá café']);
  for (let i = 0; i < 10; i++)
    rememberWorkspace('w' + i);
  assert.equal(recentWorkspaces().length, 8);
  assert.equal(recentWorkspaces()[0], 'w9');
  forgetWorkspace('w9');
  assert.equal(recentWorkspaces()[0], 'w8');

  store['webnote.recent'] = '{"not": "a list"}';
  assert.deepEqual(recentWorkspaces(), []);
  store['webnote.recent'] = '["ok", 3, null]';
  assert.deepEqual(recentWorkspaces(), ['ok']);

  globalThis.localStorage = {
    getItem() { throw new Error('blocked'); },
    setItem() { throw new Error('blocked'); },
  };
  assert.deepEqual(recentWorkspaces(), []);
  rememberWorkspace('x');  // doesn't throw
  delete globalThis.localStorage;
  assert.deepEqual(recentWorkspaces(), []);
});
