/**
 * Note text to HTML (see Note.prototype.getHTML in webnote.js).
 *
 * Notes are Markdown, plus the old wiki syntax (a line starting with !
 * is a heading, web addresses become links, a \ at the end of a line joins
 * it to the next), plus the bits of HTML notes could use before
 * (legacyToMarkdown).
 *
 * Safety: the HTML is built here, from escaped text and a fixed set of
 * tags. Stored text never reaches the HTML parser as markup. Links need an
 * http, https, ftp or mailto URL, and images an http or https one.
 *
 * Nothing here uses the DOM: decode (character references to text) is
 * passed in, so client_test.js runs this in node. Every loop is linear
 * in the text, or bounded (MAX_DEPTH), because notes can be large.
 */

const LINK_URL = /^(?:(?:https?|ftp):\/\/|mailto:)[^\s\x00-\x1f<>"]+$/i;
const IMAGE_URL = /^https?:\/\/[^\s\x00-\x1f<>"]+$/i;
// Nested emphasis, links and quotes deeper than this are left as text.
const MAX_DEPTH = 16;

const ESCAPES = {'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'};
export function escapeHTML(s) {
  return s.replace(/[&<>"']/g, c => ESCAPES[c]);
}

/// Legacy HTML

// Elements whose content is dropped along with them (when closed).
const DROP_CONTENT = new Set(['script', 'style', 'head', 'title', 'template', 'iframe',
                              'object', 'svg', 'math', 'noscript', 'select']);
// Elements that start and end a line.
const BLOCKS = new Set([
  'address', 'article', 'aside', 'blockquote', 'body', 'center', 'dd', 'details', 'dialog',
  'dir', 'div', 'dl', 'dt', 'fieldset', 'figcaption', 'figure', 'footer', 'form', 'frameset',
  'header', 'hgroup', 'html', 'legend', 'main', 'menu', 'nav', 'ol', 'p', 'pre', 'search',
  'section', 'summary', 'table', 'tbody', 'tfoot', 'thead', 'tr', 'ul']);
// Other HTML elements: their tags are dropped and their content kept. Tags
// that aren't HTML, like <customer>, stay as text.
const INLINE = new Set([
  'abbr', 'area', 'audio', 'base', 'basefont', 'bdi', 'bdo', 'big', 'blink', 'button', 'canvas',
  'caption', 'cite', 'col', 'colgroup', 'data', 'datalist', 'dfn', 'embed', 'font', 'frame',
  'input', 'ins', 'kbd', 'label', 'link', 'map', 'mark', 'marquee', 'meta', 'meter', 'nobr',
  'noframes', 'optgroup', 'option', 'output', 'param', 'picture', 'progress', 'q', 'rp', 'rt',
  'ruby', 'samp', 'slot', 'small', 'source', 'span', 'sub', 'sup', 'td', 'textarea', 'th',
  'time', 'track', 'u', 'var', 'video', 'wbr', 'xmp']);
// Pairs that become Markdown emphasis.
const EMPHASIS = {b: '**', strong: '**', i: '*', em: '*', s: '~~', strike: '~~', del: '~~',
                  code: '`', tt: '`'};

function isHTMLElement(name) {
  return DROP_CONTENT.has(name) || BLOCKS.has(name) || INLINE.has(name) || name in EMPHASIS
      || /^(?:a|br|hr|img|li|h[1-6])$/.test(name)
      // Word's <o:p>, custom elements
      || name.includes(':') || name.includes('-');
}

/**
 * The tag at s[i] (a '<'), or null if it isn't one, in which case the '<'
 * is text. A tag must be complete, with a name (letters first), and
 * attributes in quotes must close.
 * @return {{end, name, close, attrs}} end is the index after the tag;
 *   name is '' for comments and declarations
 */
export function parseTag(s, i) {
  if (s.startsWith('<!--', i)) {
    const end = s.indexOf('-->', i + 4);
    return end < 0 ? null : {end: end + 3, name: '', close: false, attrs: {}};
  }
  if ('!' == s[i + 1] || '?' == s[i + 1]) {
    const end = s.indexOf('>', i);
    return end < 0 ? null : {end: end + 1, name: '', close: false, attrs: {}};
  }
  const m = /<(\/?)([a-zA-Z][a-zA-Z0-9-]*(?::[a-zA-Z][a-zA-Z0-9-]*)?)/y;
  m.lastIndex = i;
  const found = m.exec(s);
  if (!found)
    return null;
  let j = m.lastIndex;
  if (j < s.length && !/[\s\/>]/.test(s[j]))
    return null;  // e.g. <http://...>
  const attrs = {};
  while (j < s.length) {
    while (j < s.length && /\s/.test(s[j]))
      j++;
    if ('>' == s[j])
      return {end: j + 1, name: found[2].toLowerCase(), close: '/' == found[1], attrs};
    if ('/' == s[j]) {
      j++;
      continue;
    }
    const start = j;
    while (j < s.length && !/[\s\/>="']/.test(s[j]))
      j++;
    if (start == j) {
      j++;  // a stray quote or =
      continue;
    }
    const name = s.slice(start, j).toLowerCase();
    let k = j;
    while (k < s.length && /\s/.test(s[k]))
      k++;
    if ('=' != s[k]) {
      attrs[name] = '';
      continue;
    }
    k++;
    while (k < s.length && /\s/.test(s[k]))
      k++;
    if ('"' == s[k] || "'" == s[k]) {
      const close = s.indexOf(s[k], k + 1);
      if (close < 0)
        return null;
      attrs[name] = s.slice(k + 1, close);
      j = close + 1;
    } else {
      j = k;
      while (j < s.length && !/[\s>]/.test(s[j]))
        j++;
      attrs[name] = s.slice(k, j);
    }
  }
  return null;
}

/**
 * Split text into text and tag tokens. HTML elements' tags become tags;
 * anything else, including tags that aren't HTML, stays text.
 */
function tokenize(s) {
  const tokens = [];
  let text = '';
  let i = 0;
  while (i < s.length) {
    const lt = s.indexOf('<', i);
    if (lt < 0) {
      text += s.slice(i);
      break;
    }
    text += s.slice(i, lt);
    const tag = parseTag(s, lt);
    if (tag && ('' == tag.name || isHTMLElement(tag.name))) {
      if (text)
        tokens.push({text});
      text = '';
      tag.source = s.slice(lt, tag.end);
      tokens.push(tag);
      i = tag.end;
    } else {
      text += '<';
      i = lt + 1;
    }
  }
  if (text)
    tokens.push({text});
  return tokens;
}

/**
 * Match the tags that convert() pairs up (<a>, emphasis and the elements
 * whose content is dropped), in one pass. <b> and <strong> are the same
 * kind, and so on.
 * @return {Object<number, number>} the closing token's index by the
 *   opening one's
 */
function matchTags(tokens) {
  const match = {};
  const open = {};  // kind -> stack of indexes
  tokens.forEach(function(t, k) {
    const kind = 'a' == t.name || DROP_CONTENT.has(t.name) ? t.name : EMPHASIS[t.name];
    if (!kind)
      return;
    const stack = open[kind] || (open[kind] = []);
    if (!t.close) {
      stack.push(k);
    } else if (stack.length) {
      match[stack.pop()] = k;
    }
  });
  return match;
}

// For [text](url): unmatched brackets in the text, and spaces and
// parentheses in the URL, would end them early. Matched brackets stay, so
// an image can be a link's text.
function linkText(s) {
  s = s.replace(/\\/g, '\\\\');
  const unmatched = new Set();
  const open = [];
  for (let i = 0; i < s.length; i++) {
    if ('\\' == s[i]) {
      i++;
    } else if ('[' == s[i]) {
      open.push(i);
    } else if (']' == s[i]) {
      if (open.length)
        open.pop();
      else
        unmatched.add(i);
    }
  }
  open.forEach(i => unmatched.add(i));
  let out = '';
  for (let i = 0; i < s.length; i++)
    out += (unmatched.has(i) ? '\\' : '') + s[i];
  return out;
}
function linkURL(s) {
  return s.replace(/[ ()]/g, c => '%' + c.charCodeAt(0).toString(16).toUpperCase());
}

/**
 * Convert the HTML notes could hold before to Markdown: <a> and <img> with
 * web addresses, <b>, <i> and friends, <br>, headings, list items and
 * paragraphs. Scripts, styles and comments go; other HTML tags go and
 * their text stays. Anything that only looks like a tag (<customer>, or an
 * unclosed <b>, <a> or <script>) stays as text. Character references in the text are
 * decoded.
 * @param {string} text
 * @param {function(string): string} decode character references to text
 * @return {string} Markdown
 */
export function legacyToMarkdown(text, decode) {
  if (!text.includes('<'))
    return decode(text);
  const tokens = tokenize(text);
  return convert(tokens, matchTags(tokens), 0, tokens.length, decode, 0);
}

function convert(tokens, match, from, to, decode, depth) {
  let out = '';
  // Start a line, unless one just started.
  const newline = () => {
    if (out && !out.endsWith('\n'))
      out += '\n';
  };
  for (let k = from; k < to; k++) {
    const t = tokens[k];
    if (undefined !== t.text) {
      out += decode(t.text);
      continue;
    }
    const name = t.name;
    if ('' == name || t.close && !BLOCKS.has(name) && !/^(?:li|h[1-6])$/.test(name))
      continue;  // comments, and closing tags of pairs not found below
    if (DROP_CONTENT.has(name)) {
      const close = match[k];
      if (close < to) {
        k = close;
      } else if (!t.close) {
        out += t.source;  // unclosed, so maybe not meant as a tag
      }
      continue;
    }
    if ('br' == name) {
      out += '\n';
    } else if ('hr' == name) {
      newline();
      out += '---\n';
    } else if (/^h[1-6]$/.test(name)) {
      newline();
      if (!t.close)
        out += '#'.repeat(Number(name[1])) + ' ';
    } else if ('li' == name) {
      newline();
      if (!t.close)
        out += '- ';
    } else if (BLOCKS.has(name)) {
      newline();
    } else if ('img' == name) {
      const src = decode(t.attrs.src || '').trim().replace(/ /g, '%20');
      if (IMAGE_URL.test(src))
        out += '![' + linkText(decode(t.attrs.alt || '')) + '](' + linkURL(src) + ')';
    } else if ('a' == name || name in EMPHASIS) {
      const close = match[k];
      if (!(close < to)) {
        out += t.source;  // unclosed, so maybe not meant as a tag
        continue;
      }
      if (depth >= MAX_DEPTH)
        continue;  // drop the tag, keep its text
      const inner = convert(tokens, match, k + 1, close, decode, depth + 1);
      k = close;
      const core = inner.trim();
      const lead = inner.slice(0, inner.length - inner.trimStart().length);
      const trail = inner.slice(inner.trimEnd().length);
      if ('a' == name) {
        // Browsers encoded spaces in URLs.
        const href = decode(t.attrs.href || '').trim().replace(/ /g, '%20');
        if (!LINK_URL.test(href) || core.includes('\n')) {
          out += inner;
          if (LINK_URL.test(href) && core)
            out += ' <' + href + '>';
        } else if (!core || core == href) {
          out += lead + '<' + href + '>' + trail;
        } else {
          out += lead + '[' + linkText(core) + '](' + linkURL(href) + ')' + trail;
        }
      } else if (!core || core.includes('\n')) {
        out += inner;
      } else {
        const mark = EMPHASIS[name];
        out += lead + mark + core + mark + trail;
      }
    }
    // other HTML tags: dropped
  }
  return out;
}

/// Markdown

/**
 * Render Markdown (and the wiki syntax) as HTML. Lines end with <br />,
 * as notes always have, rather than joining into paragraphs.
 * @param {string} text Markdown, already decoded (see legacyToMarkdown)
 * @return {string} HTML
 */
export function renderMarkdown(text) {
  return renderBlocks(text.replace(/\\\n/g, '').split('\n'), 0);
}

const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const ATX = /^ {0,3}(#{1,6}) +(.+?)(?: +#+)? *$/;
const RULE = /^ {0,3}([-*_])(?: *\1){2,} *$/;
// A quote needs a space after the >: notes use >>> and >this as arrows.
// "> > x" nests.
const QUOTE = /^ {0,3}>(?: (.*))?$/;
const ITEM = /^( *)([-*+]|\d{1,9}[.)]) +(.*)$/;
const LEADING_IMAGE = /^!\[[^\]]*\]\(/;

function renderBlocks(lines, depth) {
  let out = '';
  let run = null;  // the current lines of text
  const flush = followedByBlock => {
    if (run) {
      out += run.map(line => inline(line, false, depth)).join('<br />');
      // A line before a block ends with a <br /> too, so a blank line
      // between them still shows.
      if (followedByBlock)
        out += '<br />';
    }
    run = null;
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let m;
    if (depth < MAX_DEPTH && (m = FENCE.exec(line))) {
      flush(true);
      const code = [];
      let j = i + 1;
      while (j < lines.length && !lines[j].trimStart().startsWith(m[1]))
        code.push(lines[j++]);
      out += '<pre><code>' + escapeHTML(code.join('\n')) + '</code></pre>';
      i = j;
    } else if ((m = ATX.exec(line))) {
      flush(true);
      const level = m[1].length;
      out += '<h' + level + '>' + inline(m[2], false, depth) + '</h' + level + '>';
    } else if ('!' == line[0] && !LEADING_IMAGE.test(line)) {
      // Wiki headings: more !s are bigger, up to h1.
      flush(true);
      let c = 0;
      while ('!' == line[c])
        c++;
      const level = 4 - Math.min(c, 3);
      out += '<h' + level + '>' + inline(line.slice(c), false, depth) + '</h' + level + '>';
    } else if (RULE.test(line)) {
      flush(true);
      out += '<hr />';
    } else if (depth < MAX_DEPTH && QUOTE.test(line)) {
      flush(true);
      const quoted = [];
      let j = i;
      while (j < lines.length && (m = QUOTE.exec(lines[j]))) {
        quoted.push(m[1] || '');
        j++;
      }
      out += '<blockquote>' + renderBlocks(quoted, depth + 1) + '</blockquote>';
      i = j - 1;
    } else if (depth < MAX_DEPTH && (m = ITEM.exec(line))) {
      flush(true);
      i = renderList(lines, i, depth, html => { out += html; }) - 1;
    } else {
      if (!run)
        run = [];
      run.push(line);
    }
  }
  flush(false);
  return out;
}

/**
 * Render the list starting at lines[start]: items of the same kind (bullets
 * or numbers) at about the same indent. More indented lines belong to the
 * item above, and can be lists themselves. A blank line ends the list.
 * @return the index of the first line after the list
 */
function renderList(lines, start, depth, emit) {
  const first = ITEM.exec(lines[start]);
  const indent = first[1].length;
  const ordered = /\d/.test(first[2]);
  const number = parseInt(first[2], 10);
  let html = !ordered ? '<ul>' : (1 == number ? '<ol>' : '<ol start="' + number + '">');
  let i = start;
  while (i < lines.length) {
    const m = ITEM.exec(lines[i]);
    if (!m || m[1].length >= indent + 2 || /\d/.test(m[2]) != ordered)
      break;
    const children = [];
    let j = i + 1;
    while (j < lines.length && lines[j].trim()
           && lines[j].length - lines[j].trimStart().length >= indent + 2) {
      children.push(lines[j].slice(indent + 2));
      j++;
    }
    html += '<li>' + inline(m[3], false, depth);
    if (children.length) {
      const inner = renderBlocks(children, depth + 1);
      html += (/^ {0,3}(?:[-*+]|\d{1,9}[.)]) /.test(children[0]) ? '' : '<br />') + inner;
    }
    html += '</li>';
    i = j;
  }
  emit(html + (ordered ? '</ol>' : '</ul>'));
  return i;
}

// The old automatic links: a web address at the start of the text or after
// whitespace.
const BARE_URL = /(?:https?|ftp):\/\/[-a-zA-Z0-9%.~:_\/]+[a-zA-Z\/](?:[?&][-a-zA-Z0-9%.~:_]+=[-a-zA-Z0-9%.~:_]+)*(?:#[-a-zA-Z0-9%.~:_]*)?/y;
const PUNCTUATION = /[!-\/:-@\[-`{-~]/;

function link(url, html) {
  return '<a href="' + escapeHTML(url) + '">' + html + '</a>';
}

/**
 * The closing brackets matching each '[' in s (by index), skipping
 * backslash escapes.
 */
function matchBrackets(s) {
  const match = {};
  const open = [];
  for (let i = 0; i < s.length; i++) {
    if ('\\' == s[i]) {
      i++;
    } else if ('[' == s[i]) {
      open.push(i);
    } else if (']' == s[i] && open.length) {
      match[open.pop()] = i;
    }
  }
  return match;
}

/**
 * The link destination at s[i] (just after the '('): the URL, then an
 * optional title, then ')'.
 * @return {{url, end}} or null
 */
function parseDestination(s, i) {
  const m = /[ \t]*(?:<([^<>\n]*)>|((?:[^\s()\\]|\\.|\((?:[^\s()\\]|\\.)*\))*))(?:[ \t]+(?:"[^"]*"|'[^']*'))?[ \t]*\)/y;
  m.lastIndex = i;
  const found = m.exec(s);
  if (!found)
    return null;
  const url = (undefined !== found[1] ? found[1] : found[2]).replace(/\\(.)/g, '$1');
  return {url, end: m.lastIndex};
}

/**
 * Can delim at s[j] close emphasis?  It must be a whole run, after a
 * non-space, and for _, not followed by a letter or digit.
 */
function isCloser(s, j, delim) {
  const after = s[j + delim.length] || '';
  return j > 0 && !/\s/.test(s[j - 1]) && delim[0] != s[j - 1] && delim[0] != after
      && !('_' == delim[0] && /[\p{L}\p{N}]/u.test(after));
}

/** Where delim first closes after s[i], or -1. */
function findCloser(s, i, delim) {
  for (let j = s.indexOf(delim, i + 1); j >= 0; j = s.indexOf(delim, j + 1)) {
    if (isCloser(s, j, delim))
      return j;
  }
  return -1;
}

/** Where delim last closes in s, or -1. */
function findLastCloser(s, delim) {
  for (let j = s.lastIndexOf(delim); j >= 0; j = j > 0 ? s.lastIndexOf(delim, j - 1) : -1) {
    if (isCloser(s, j, delim))
      return j;
  }
  return -1;
}

/**
 * Render a line's inline Markdown: emphasis, code, links, images,
 * automatic links and backslash escapes. Everything else is escaped.
 * @param {boolean} inLink in a link's text, which can't hold links
 */
function inline(s, inLink, depth) {
  if (depth >= MAX_DEPTH)
    return escapeHTML(s);
  const brackets = matchBrackets(s);
  // Where each delimiter can last close; openers after it can't, so the
  // search for a closer only runs when one may follow.
  const lastCloser = {};
  for (const d of ['***', '**', '__', '~~', '*', '_']) {
    lastCloser[d] = findLastCloser(s, d);
  }
  let out = '';
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if ('\\' == c && PUNCTUATION.test(s[i + 1] || '')) {
      out += escapeHTML(s[i + 1]);
      i += 2;
      continue;
    }
    if ('`' == c) {
      let n = 1;
      while ('`' == s[i + n])
        n++;
      const ticks = '`'.repeat(n);
      let j = s.indexOf(ticks, i + n);
      while (j >= 0 && '`' == s[j + n])
        j = s.indexOf(ticks, j + n + 1);
      if (j >= 0) {
        let code = s.slice(i + n, j);
        if (/^ .*[^ ].* $/.test(code))
          code = code.slice(1, -1);
        out += '<code>' + escapeHTML(code) + '</code>';
        i = j + n;
      } else {
        out += ticks;
        i += n;
      }
      continue;
    }
    if ('!' == c && '[' == s[i + 1] && undefined !== brackets[i + 1]
        && '(' == s[brackets[i + 1] + 1]) {
      const close = brackets[i + 1];
      const dest = parseDestination(s, close + 2);
      if (dest && IMAGE_URL.test(dest.url)) {
        const alt = s.slice(i + 2, close).replace(/\\(.)/g, '$1');
        out += '<img src="' + escapeHTML(dest.url) + '" alt="' + escapeHTML(alt) + '" />';
        i = dest.end;
        continue;
      }
    }
    if ('[' == c && !inLink && undefined !== brackets[i] && '(' == s[brackets[i] + 1]) {
      const close = brackets[i];
      const dest = parseDestination(s, close + 2);
      if (dest && LINK_URL.test(dest.url)) {
        out += link(dest.url, inline(s.slice(i + 1, close), true, depth + 1));
        i = dest.end;
        continue;
      }
    }
    if ('<' == c && !inLink) {
      const m = /<((?:(?:https?|ftp):\/\/|mailto:)[^\s<>"]*)>/y;
      m.lastIndex = i;
      const found = m.exec(s);
      if (found && LINK_URL.test(found[1])) {
        out += link(found[1], escapeHTML(found[1]));
        i = m.lastIndex;
        continue;
      }
    }
    if (!inLink && (0 == i || /\s/.test(s[i - 1])) && /[hHfF]/.test(c)) {
      BARE_URL.lastIndex = i;
      const found = BARE_URL.exec(s);
      if (found) {
        out += link(found[0], escapeHTML(found[0]));
        i = BARE_URL.lastIndex;
        continue;
      }
    }
    if ('*' == c || '_' == c || '~' == c) {
      let n = 1;
      while (c == s[i + n])
        n++;
      const html = emphasis(s, i, n, lastCloser, inLink, depth);
      if (html) {
        out += html.html;
        i = html.end;
      } else {
        out += s.substr(i, n);  // the whole run is text
        i += n;
      }
      continue;
    }
    out += escapeHTML(c);
    i++;
  }
  return out;
}

const EMPHASIS_TAGS = {'***': ['<strong><em>', '</em></strong>'],
                       '**': ['<strong>', '</strong>'], '__': ['<strong>', '</strong>'],
                       '~~': ['<del>', '</del>'], '*': ['<em>', '</em>'], '_': ['<em>', '</em>']};

/**
 * Emphasis opening at s[i], a run of n *, _ or ~: the run must be followed
 * by a non-space
 * and close later in the line (for _, outside words).
 * @return {{html, end}} or null
 */
function emphasis(s, i, n, lastCloser, inLink, depth) {
  const c = s[i];  if ('~' == c && 2 != n)
    return null;
  if (n > 3 || /\s/.test(s[i + n] || ' '))
    return null;
  if ('_' == c && i > 0 && /[\p{L}\p{N}]/u.test(s[i - 1]))
    return null;
  const delim = c.repeat(n);
  if (!(delim in lastCloser) || lastCloser[delim] <= i)
    return null;
  const close = findCloser(s, i + n - 1, delim);
  if (close < 0)
    return null;
  const tags = EMPHASIS_TAGS[delim];
  return {html: tags[0] + inline(s.slice(i + n, close), inLink, depth + 1) + tags[1],
          end: close + n};
}

/**
 * A note's HTML: its legacy HTML converted, then rendered as Markdown.
 * @param {string} text the note's stored text
 * @param {function(string): string} decode character references to text
 */
export function renderNote(text, decode) {
  return renderMarkdown(legacyToMarkdown(text, decode));
}
