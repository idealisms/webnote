// Unit tests for the parts of the client that don't need a page. Run with
// `node --test` (js_test.go does, as part of `go test`). browsertest.py
// covers the rest in real browsers.
//
// The scripts aren't modules yet, so each test loads them into a fresh
// context the way the page does (their top-level vars end up on it), with
// stand-ins for the few browser globals they touch.

'use strict';

process.env.TZ = 'Asia/Tokyo';  // Not the server's US/Eastern.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

function load() {
  const elements = {};
  const ctx = vm.createContext({
    navigator: {userAgent: 'node'},
    window: {innerWidth: 1280, innerHeight: 800},
    document: {getElementById: id => elements[id] || (elements[id] = {})},
  });
  for (const f of ['strings.js.en', 'objects.js', 'webnote.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, f), 'utf8'), ctx, {filename: f});
  }
  return ctx;
}

test('getHTML: line breaks and headings', () => {
  const {Note} = load();
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

test('getHTML: links', () => {
  const {Note} = load();
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

test('charRefs stores characters 160-255 as references', () => {
  const {charRefs} = load();
  assert.equal(charRefs('café  ÿ Ā 日 \u{1f600} &amp; <b>'),
               'caf&#233; &#160;&#255; Ā 日 \u{1f600} &amp; <b>');
});

test('myescape', () => {
  const {myescape} = load();
  assert.equal(myescape('aZ09_'), 'aZ09_');
  assert.equal(myescape('my-notes.txt'), 'my%2Dnotes%2Etxt');
  assert.equal(myescape('Bogotá café'), 'Bogot%E1%20caf%E9');
  // Unpadded %u escapes, and an astral character is two of them.
  assert.equal(myescape('י日\u{1f600}'), '%u5D9%u65E5%uD83D%uDE00');
});

test('Color parses and prints', () => {
  const {Color} = load();
  for (const value of ['rgb(255, 16, 0)', 'ff1000', '#ff1000']) {
    assert.equal(new Color(value).toString(), '#ff1000', value);
  }
});

test('Color.hsvadj darkens each note color for hover', () => {
  const {Color, bgColors} = load();
  const hover = Array.from(bgColors, c => new Color(c).hsvadj(0, 0, -0.1).toString());
  assert.deepEqual(hover, ['#e5e52b', '#809ae5', '#72e564', '#e55656',
                           '#e5a239', '#e560d7', '#e5e5e5', '#969696']);
});

test('Point', () => {
  const {Point} = load();
  const p = new Point(3, 4);
  assert.equal(p.add(new Point(1, 2)).toString(), '(4, 6)');
  assert.equal(p.sub(new Point(1, 2)).toString(), '(2, 2)');
  assert.equal(p.div(2).toString(), '(1.5, 2)');
  assert.ok(p.copy().equals(p) && p.copy() !== p);
  assert.equal(new Point().toString(), '(0, 0)');
});

test('MyDate rounds to the second and shows local time', () => {
  const {MyDate} = load();
  const d = new MyDate('1792951601500001');
  assert.equal(d.dateobj.getTime(), 1792951602000);
  assert.match(String(new MyDate('0')), /1970.*9:00:00/, 'Tokyo is UTC+9');
});

test('History: undo, redo and the toolbar', () => {
  const ctx = load();
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

test('History keeps the last 40 actions', () => {
  const ctx = load();
  const history = ctx.History;
  for (let i = 0; i < 45; i++) {
    history.add({title: String(i), undo() {}, redo() {}});
  }
  assert.equal(history.undoStack.length, 40);
  assert.equal(history.undoStack[0].title, '5');
});

test('NotePos cascades new notes and wraps at the window edge', () => {
  const ctx = load();
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
