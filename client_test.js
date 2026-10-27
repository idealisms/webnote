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

test('getHTML: line breaks and headings', async () => {
  const {Note} = await load();
  const html = text => Note.prototype.getHTML.call({text});
  assert.equal(html(''), '');
  assert.equal(html('a\nb'), 'a<br />b');
  assert.equal(html('a\\\nb'), 'ab', 'a backslash joins lines');
  assert.equal(html('x ! y'), 'x ! y');
  assert.equal(html('<b>bold</b>'), '<b>bold</b>', 'notes are HTML');
  // More !s make a bigger heading, up to h1. Quirk, pinned: the heading
  // keeps its line's <br>.
  assert.equal(html('!h3\n!!h2\n!!!h1\n!!!!h1'),
               '<h3>h3<br /></h3><h2>h2<br /></h2><h1>h1<br /></h1><h1>h1</h1>');
});

test('getHTML: links', async () => {
  const {Note} = await load();
  const html = text => Note.prototype.getHTML.call({text});
  const link = url => '<a href="' + url + '">' + url + '</a>';
  assert.equal(html('http://e.com/start'), link('http://e.com/start'));
  assert.equal(html('see https://e.com/s. ok'), 'see ' + link('https://e.com/s') + '. ok');
  assert.equal(html('a\tftp://e.com/f'), 'a\t' + link('ftp://e.com/f'));
  assert.equal(html('line\nhttp://e.com/x'), 'line<br />' + link('http://e.com/x'));
  assert.equal(html('http://e.com/a?x=1&y=2 z'), link('http://e.com/a?x=1&y=2') + ' z');
  assert.equal(html('nohttp://e.com mailto:x@e.com'), 'nohttp://e.com mailto:x@e.com');
  // Quirk, pinned: a fragment swallows a trailing '.'.
  assert.equal(html('http://e.com/a#frag. x'), link('http://e.com/a#frag.') + ' x');
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
  // The dialogs: showCollision() builds DOM, so it just opens its dialog.
  const collision = ctx.document.getElementById('collisionDialog');
  const conflict = ctx.document.getElementById('conflictDialog');
  ws.showCollision = () => { collision.open = true; };
  const answer = async (status, text) => {
    requests.shift().resolve({status, text: async () => text});
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
  assert.match(requests[0].url, /^getrecent\.py\?name=ws&epoch=1$/);
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
  assert.match(requests[0].url, /^save\.py\?epoch=1&loaded=100$/);
  const alerts = [];
  globalThis.alert = message => alerts.push(message);
  await answer(500, 'error');
  assert.equal(alerts.length, 1);

  // After an answer, saving works again.
  ws.save();
  assert.equal(requests.length, 1);
  assert.match(requests[0].url, /loaded=100$/);
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
