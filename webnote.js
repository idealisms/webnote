/**
 * @fileoverview The Webnote specific classes.<br /><br />
 *
 * The two main classes are {@link workspace} and {@link Note}.
 *
 * {@link workspace} represents the workspace as a whole.  It contains
 * refernces to all the notes and methods for operations such as saving
 * the notes or undo-ing/redo-ing an action.
 */

import {Color, MyDate, Point, db, findRelativeMousePos, get, myescape} from './objects.js';

// global variables
// user options
// Shows a box for db() messages.
const debugOn = false;
const notePadding = 5;
const noteBorder = 3;
const noteBorderColor = '#000';
const miniWidth = 51;

export const adminEmail = 'webnote@ponderer.org';
const baseURI = '/webnote/';
// How many revisions the history note lists at a time.
const numDates = 10;

// TODO: make it easier for users to make custom color maps
const colorMap = {
  '#ffff30': strings.COLOR_YELLOW,
  '#8facff': strings.COLOR_BLUE,
  '#7fff70': strings.COLOR_GREEN,
  '#ff6060': strings.COLOR_RED,
  '#ffb440': strings.COLOR_ORANGE,
  '#ff6bef': strings.COLOR_PURPLE,
  '#ffffff': strings.COLOR_WHITE,
  '#b0b0b0': strings.COLOR_GREY
};
export const bgColors = [];
for (const c in colorMap)
{
  bgColors.push(c);
}

// other global variables, you shouldn't change these
// BROWSER_SAFARI is any WebKit or Blink browser (Safari, Chrome, Edge);
// BROWSER_MOZILLA is everything else (Firefox).
const BROWSER_SAFARI = 2;
const BROWSER_MOZILLA = 3;
const browser = navigator.userAgent.toLowerCase().indexOf('webkit') != -1
    ? BROWSER_SAFARI : BROWSER_MOZILLA;

/**
 * Create a new note object.  You shouldn't need to call this directly, use
 * {@link workspace#createNote} instead.
 * @class A class that acts as a wrapper to the actual html div that is a
 * note.  Rather than operating directly on the html object, use this
 * class instead.
 * TODO: attach these methods directly to the html objects.
 * @param {HtmlElement} note a reference to the actual note or a string that
 * is the id of the note
 * @param {workspace} p a reference to the parent workspace
 * @param {string} text the text of the note
 */
export function Note(note, p, text) {
  // constructor code
  if (typeof note == 'string')
    note = get(note);
  if (!text) text = ''
  //
  /**
   * TODO: fix references to workspace with this.parent
   * @type workspace
   */
  this.parent = p;
  /**
   * Whether or not is it possible to click on a note and select it
   * @type boolean
   */
  this.selectable = true;
  /**
   * Whether or not we can double click and edit a note
   * @type boolean
   */
  this.editable = true;
  /**
   * The html id of the note
   * @type string
   */
  this.id = note.id;
  /**
   * The height and width of the note
   * @type Point
   */
  this.size = new Point(parseInt(note.style.width), parseInt(note.style.height));

  // create the mini note dom object
  const dotElt = document.createElement('div');
  dotElt.id = 'm' + this.id;
  dotElt.addEventListener('mousedown', this.miniMouseDown.bind(this));
  get('mini').appendChild(dotElt);

  const dot = get('m' + this.id);
  dot.className = 'mininote';
  dot.style.backgroundColor = note.style.backgroundColor;

  // call methods
  this.setColor(note.style.backgroundColor, true);
  this.setText(text);
  this.updateSize();
}

/**
 * User clicked on a note.
 */
Note.prototype.mouseDown = function(ev) {
  const target = ev.target;
  if (!this.selectable) {
    ev.preventDefault();
    return;
  }

  // alt + rt click == send to back
  if (ev.altKey && 2 == ev.button) {
    this.sendToBack();
    ev.preventDefault();
    return;
  }

  const lbutton = 0 == ev.button;

  // only the left button should have an event
  // likewise we can disable actions by using meta or ctrl+alt
  if (!lbutton || ev.metaKey || (ev.ctrlKey && ev.altKey))
    return;

  // Determine if we're clicking on the note or a scrollbar
  const pos = findRelativeMousePos(ev);
  const size = this.getCorrectedSize();

  if (!target.clientWidth || // no clientWidth if you click on a scrollbar in FF1.0
      (pos.x > target.clientWidth + noteBorder
       && pos.x < size.x - noteBorder) ||
      (pos.y > target.clientHeight + noteBorder
       && pos.y < size.y - noteBorder)) {
    // a scrollbar: let the browser scroll the note
    ev.stopPropagation();
    return;
  }

  this.parent.mouse.select(this, ev);
  ev.stopPropagation();
  ev.preventDefault();
}

/**
 * User clicked on one of the mini notes at the top of the page.
 */
Note.prototype.miniMouseDown = function(ev)
{
  // bring all notes of the same color to the front
  if (ev.ctrlKey) {
    for (const n in this.parent.notes) {
      const note = this.parent.notes[n];
      if (note != this && note.bgColor.toString() == this.bgColor.toString())
        note.select();
    }
  } else if (ev.shiftKey) {
    workspace.filter(strings.COLOR + ":" + colorMap[this.bgColor.toString()]);
  }
  this.select();

  // bring it back if it's off screen
  const size = this.getCorrectedSize();
  const pos = this.getPosition();
  const elt = get(this.id);
  if (size.x + pos.x < 5)
    elt.style.left = "1px";
  if (size.y + pos.y < 5)
    elt.style.top = "1px";
}
/**
 * User deselected a note (stopped dragging).
 */
Note.prototype.mouseUp = function() { this.parent.mouse.deselect(); }
/**
 * Note keyboard events.
 */
Note.prototype.keyDown = function(ev)
{
  // don't do anything if we're editing the note
  if (this.parent.edit == this)
    return;

  // set the color based on number
  const idx = parseInt(String.fromCharCode(ev.keyCode)) - 1;
  if (idx >= 0 && idx < bgColors.length)
    this.setColor(bgColors[idx]);
}
/**
 * Moving the mouse while over a note (changes cursor).
 */
Note.prototype.mouseMove = function(ev)
{
  const elt = get(this.id);
  if (!this.selectable)
  {
    elt.style.cursor = 'auto';
    return;
  }
  if (this.parent.mouse.selObj)
    return;

  let top = false;
  let left = false;
  let right = false;
  let bottom = false;
  let resize_cursor = '';
  const size = this.getCorrectedSize();
  const relPos = findRelativeMousePos(ev);

  if (relPos.y <= noteBorder) {
    top = true;
    resize_cursor += 'n';
  } else if (size.y - relPos.y <= noteBorder) {
    bottom = true;
    resize_cursor += 's';
  }

  if (relPos.x <= noteBorder) {
    left = true;
    resize_cursor += 'w';
  } else if (size.x - relPos.x <= noteBorder) {
    right = true;
    resize_cursor += 'e';
  }

  if (resize_cursor == '') {
    elt.style.cursor = 'move';
  } else {
    elt.style.cursor = resize_cursor + '-resize';
  }

  this.parent.mouse.notePosRel['top'] = top;
  this.parent.mouse.notePosRel['bottom'] = bottom;
  this.parent.mouse.notePosRel['left'] = left;
  this.parent.mouse.notePosRel['right'] = right;
}
/**
 * Mouse moves over a note (darken the background color).
 */
Note.prototype.mouseOver = function() {
  const elt = get(this.id);
  elt.style.background = (new Color(this.bgColor.toString())).hsvadj(0, 0, -0.1);
  this.parent.mouse.noteOver = this;
}
/**
 * Mouse leaves a note (restore original background color).
 */
Note.prototype.mouseOut = function() {
  const elt = get(this.id);
  elt.style.backgroundColor = this.bgColor.toString();
  delete this.parent.mouse.noteOver;
}
/**
 * Double-click event on a note (try to toggle edit mode).
 */
Note.prototype.mouseDblClick = function() {
  if (!this.editable)
    return;

  if (this.parent.edit == this) {
    this.parent.editOff();
    return;
  }
  this.parent.editOff();
  const pSize = this.getCorrectedSize();
  pSize.x -= 2 * (noteBorder + notePadding + 1);
  pSize.y -= 2 * (noteBorder + notePadding + 1) + 20;
  const bar = document.createElement('div');
  bar.style.textAlign = 'right';
  bar.style.margin = '0 2px 1px 0';

  // color swatches here
  bgColors.forEach(function(color, i) {
    const swatch = document.createElement('div');
    swatch.style.cssText = 'width: 12px;height:12px;font-size:1px;float:left;'
        + 'border: 1px #000 solid; margin:0 1px 1px 0;cursor:auto;';
    swatch.style.background = color;
    swatch.title = strings.COLOR_SWATCH_TOOLTIP.replace('$1', i + 1);
    swatch.addEventListener('mousedown', function(ev) {
      this.setColor(color);
      ev.stopPropagation();
    }.bind(this));
    bar.appendChild(swatch);
  }, this);

  // A red X, drawn corner to corner and clipped square at the edges. The
  // shadow keeps it visible on red notes.
  const svgNS = 'http://www.w3.org/2000/svg';
  const close = document.createElementNS(svgNS, 'svg');
  close.setAttribute('class', 'closeNote');
  close.setAttribute('viewBox', '0 0 12 12');
  close.setAttribute('width', '12');
  close.setAttribute('height', '12');
  close.setAttribute('role', 'img');
  close.setAttribute('aria-label', strings.CLOSE_ICON_ALT);
  close.style.cssText = 'cursor:auto;filter:drop-shadow(0.5px 1px 0.5px rgba(0,0,0,0.6));';
  const tooltip = document.createElementNS(svgNS, 'title');
  tooltip.textContent = strings.CLOSE_ICON_TOOLTIP;
  const x = document.createElementNS(svgNS, 'path');
  x.setAttribute('d', 'M0 0L12 12M12 0L0 12');
  x.setAttribute('stroke', '#f00');
  x.setAttribute('stroke-width', '2.6');
  close.append(tooltip, x);
  close.addEventListener('click', this.destroy.bind(this, true));
  bar.appendChild(close);

  const textbox = document.createElement('textarea');
  textbox.id = this.id + 'text';
  textbox.style.width = pSize.x + 'px';
  textbox.style.height = pSize.y + 'px';
  textbox.addEventListener('mousedown', stopPropagation);
  textbox.addEventListener('dblclick', stopPropagation);
  textbox.value = decodeCharRefs(this.text);

  const elt = get(this.id);
  elt.replaceChildren(bar, textbox);
  elt.title = '';
  elt.style.overflow = 'hidden';
  get(this.id + 'text').focus();
  this.parent.edit = this;
}
/**
 * Destroy the note (user clicked on the X).
 * @param {boolean} addToHistory should we add information to the undo
 * stack?
 */
Note.prototype.destroy = function(addToHistory)
{
  // if it's being edited, turn it off
  if (this.parent.edit == this)
    this.parent.editOff();

  let elt = get(this.id);

  // save undo information
  if (addToHistory)
  {
    const pos = this.getPosition();
    const ws = this.parent;
    const f = {
      title : strings.HISTORY_DELETE_NOTE,
      noteData : {
          'id' : this.id,
          'xPos' : pos.x,
          'yPos' : pos.y,
          'height' : this.size.y,
          'width' : this.size.x,
          'bgcolor' : this.bgColor.toString(),
          'zIndex' : elt.style.zIndex,
          'text' : this.text
        },
      undo : function() {
        ws.createNote(this.noteData);
      },
      redo : function() {
        ws.notes[this.noteData.id].destroy(false);
      }
    };
    this.parent.history.add(f);
  }

  // now delete the html node
  elt.parentNode.removeChild(elt);

  // delete the mini html node
  elt = get('m' + this.id);
  elt.parentNode.removeChild(elt);

  // remove it from the array of notes
  delete this.parent.notes[this.id];
  this.parent.numNotes--;
  this.parent.changed = true;

  // resize the mini box and update the stats
  this.parent.updateMiniBox();

  // is the garbage collector smart enough to delete the object now?
}
/**
 * Get the coordinates of the upper left corner of the note.
 * @type Point
 */
Note.prototype.getPosition = function()
{
  let ret = new Point(0, 0);
  const elt = get(this.id);
  ret = new Point(parseInt(elt.style.left), parseInt(elt.style.top));
  return ret;
}
/**
 * Get the size of the note according to the dom object (this varies on
 * browser).
 * @type Point
 */
Note.prototype.getSize = function()
{
  return new Point(this.size.x, this.size.y);
}
/**
 * Get the size of the note including border and padding.
 * @type Point
 */
Note.prototype.getCorrectedSize = function() {
  const ret = this.getSize();
  const offset = 2*(noteBorder+notePadding);
  ret.x += offset;
  ret.y += offset;
  return ret;
}

/**
 * Set the color of a note.
 * @param {string} hex the color in hex
 * @param {boolean} ignoreHistory should we add it to the history?
 * Different default values makes this inconsistent with {@link #destroy}
 */
Note.prototype.setColor = function(hex, ignoreHistory)
{
  const newColor = new Color(hex);

  if (!ignoreHistory)
  {
    const f = {
      title : strings.HISTORY_DELETE_NOTE,
      note : this,
      ucolor : this.bgColor,
      rcolor : newColor,
      undo : function()
      {
        this.note.setColor(this.ucolor.toString(), true);
      },
      redo : function()
      {
        this.note.setColor(this.rcolor.toString(), true);
      }
    }
    this.parent.history.add(f);
  }

  this.bgColor = newColor;

  const elt = get(this.id);
  if (this.parent.mouse.noteOver && this.parent.mouse.noteOver == this)
    elt.style.background = (new Color(this.bgColor.toString())).hsvadj(0, 0, -0.1);
  else
    elt.style.background = this.bgColor.toString();

  get('m' + this.id).style.background = this.bgColor;
}
/**
 * Convert the text of a note to html.  We perform a simple transform of
 * newlines into <br/> and ! into headings.  Other wiki/textile like
 * transforms would happen here.
 * @type string
 */
Note.prototype.getHTML = function() // wikification
{
  let sCopy = this.text.replace(/\n/g,"<br />\n");
  sCopy = sCopy.replace(/\\<br \/>\n/g,"\n");

  // Turn URLs into links: at the start of the text or after whitespace,
  // which stays (a newline here is the end of a line).
  sCopy = sCopy.replace(/(^|\s)((https?|ftp)(:\/\/[-a-zA-Z0-9%.~:_\/]+[a-zA-Z\/])([?&]([-a-zA-Z0-9%.~:_]+)=([-a-zA-Z0-9%.~:_])+)*(#([-a-zA-Z0-9%.~:_]+)?)?)/g,
                        '$1<a href="$2">$2</a>');
  const lines = sCopy.split('\n');
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].length > 0) {
      // handle headings
      switch(lines[i].charAt(0)) {
        case '!': // headings
          let c = 0;
          while ('!' == lines[i].charAt(c))
            c++;
          lines[i] = lines[i].substring(c);
          c = Math.min(c, 3); // h3 is the biggest
          c = 4 - c;
          lines[i] = '<h' + c + '>' + lines[i] + '</h' + c + '>';
          break;
        default:
          // lines[i] = lines[i] + '<br />';
      }
    }
  }
  return lines.join('');
}
/**
 * Generate the xml representing a note (used when we save notes).
 * @type string
 */
Note.prototype.toXML = function()
{
  let ret = "<note noteid='" + this.id + "'";
  ret += " bgcolor='" + this.bgColor + "'";
  ret += " xposition='" + this.getPosition().x + "'";
  ret += " yposition='" + this.getPosition().y + "'";
  ret += " height='" + this.getSize().y + "'";
  ret += " width='" + this.getSize().x + "'";
  ret += " zindex='" + get(this.id).style.zIndex + "'";
  // Use the native escape() for text: unescape() can't decode myescape()'s
  // unpadded %u escapes (e.g. %u5D9).
  ret += ">\n" + escape(this.text) + "\n";
  return ret + "</note>"
}
/**
 * Change the text of a note.
 * @param {string} str the text for the note.
 */
Note.prototype.setText = function(str)
{
  str = charRefs(str);

  if (str != this.text)
  {
    this.parent.changed = true;
    this.text = str;
  }
  const elt = get(this.id);
  elt.innerHTML = this.getHTML();

  // make images undraggable
  const imgs = elt.getElementsByTagName('img');
  for (let i = 0; i < imgs.length; i++) {
    imgs[i].addEventListener('mousedown', preventDefault);
  }

  elt.title = strings.NOTE_TOOLTIP;
  get('m' + this.id).title = str;
}
/**
 * We keep track of the size of the note internally; this method updates
 * that value.
 */
Note.prototype.updateSize = function()
{
  const elt = get(this.id);
  this.size.x = parseInt(elt.style.width);
  this.size.y = parseInt(elt.style.height);
}
/**
 * Disable a note (can't be moved or edited).
 */
Note.prototype.disable = function()
{
  this.selectable = this.editable = false;
  const elt = get(this.id);
  elt.title = '';
}
/**
 * Enable a note (can be moved and edited).
 */
Note.prototype.enable = function()
{
  this.selectable = this.editable = true;
  const elt = get(this.id);
  elt.title = strings.NOTE_TOOLTIP;
}
/**
 * When a note is selected from the mini note toolbar, we bring it to the
 * front and flash the background.
 */
Note.prototype.select = function()
{
  this.parent.reZOrder(this.id);
  const self = this;
  const elt = get(this.id);
  elt.style.backgroundColor = (new Color(this.bgColor.toString()))
                                   .hsvadj(0, 0, -0.1);
  // make a new layer with the name
  const d = document.createElement('div');
  d.textContent = this.id;
  d.style.position = 'absolute';
  d.style.top = (parseInt(elt.style.top) + 5) + 'px';
  d.style.left = (parseInt(elt.style.left) + 5) + 'px';
  d.style.zIndex = 1000;
  d.style.backgroundColor = '#fff';
  d.style.padding = '4px';
  d.style.opacity = 0.8;
  get('content').appendChild(d);

  setTimeout(function() {
    elt.style.backgroundColor = self.bgColor.toString();
    d.parentNode.removeChild(d);
  }, 500);
}
/**
 * Send a note to the back of the workspace.
 */
Note.prototype.sendToBack = function()
{
  const elt = get(this.id);
  elt.style.zIndex = 0;
  this.parent.reZOrder();
}
/**
 * Move the note relative to its current position.
 * @param {Point} delta a point object
 */
Note.prototype.move = function(delta) {
  const newpos = this.getPosition().add(delta);
  const elt = get(this.id);
  elt.style.left = newpos.x + 'px';
  elt.style.top = newpos.y + 'px';
}

/**
 * Determine which note is above which other note.  Used when re-stacking
 * notes.  Returns -1 if a is below b, +1 if a is above b, and 0 if they
 * have the same z value.
 * @param {Note} a
 * @param {Note} b
 * @type int
 */
function cmpNotesZ(a, b)
{
  const av = parseInt(get(a.id).style.zIndex);
  const bv = parseInt(get(b.id).style.zIndex);
  if (av < bv)
    return -1;
  if (av > bv)
    return 1;
  return 0;
}

//
// Mouse objects
//

/**
 * @class A class that tracks the mouse position and handles mouse events.
 * @constructor
 */
const Mouse =
{
  /**
   * When resizing a note, this determines which edges need to be moved and
   * which edges remain fixed.  It is a dictionary from string -> boolean
   * where the string is either 'top, 'right', 'bottom' or 'left' and true
   * means the edge is moving.
   * @see Note#mouseMove
   * @type dictionary
   */
  notePosRel : {},
  /**
   * The current location of the mouse.  We initialize it to a dummy value
   * because object.js probably hasn't loaded yet (and Point is undefined).
   * It actually gets set in {@link GLOBALS#init}
   * @type Point
   */
  curPos : 0,

  /**
   * Update the mouse position and resize/move notes if necessary.
   * @param {int} x x position
   * @param {int} y y position
   */
  update : function(x, y)
  {
    this.curPos.x = x;
    this.curPos.y = y;

    // if something is selected
    if (this.selObj)
      this.selObj.update(this);
  },
  /**
   * Select a note either for dragging or for resizing.
   * @param {Note} note the selected note
   * @param {event} ev the javascript event object
   */
  select : function(note, ev) {
    if (this.selObj) // something already selected
      return;

    if (get(note.id).style.cursor != "move")
      this.selObj = new SelectedObjectResize(note, this.notePosRel);
    else
    {
      if (ev.altKey)
      {
        this.selObj = new SelectedObjectResize(note,
              {'top': false, 'bottom':true, 'left':false, 'right':true});
      }
      else if (ev.ctrlKey)
        this.selObj = new SelectedObjectDrag(note, true)
      else
        this.selObj = new SelectedObjectDrag(note, false);
    }
    this.downPos = this.curPos.copy();

    // move the selected item to the top
    workspace.reZOrder(note.id);
  },
  /**
   * Release selected notes.
   */
  deselect : function()
  {
    if (this.selObj)
    {
      this.selObj.deselect();
      delete this.selObj;
    }
  }
};


/**
 * Create a new Dragging object.
 * @class A class that contains information about note(s) that is being
 * dragged.
 * @see Mouse#select
 * @param {Note} note a reference to the note being dragged
 * @param {boolean} isGroup are we dragging all notes of the same color?
 * @constructor
 */
function SelectedObjectDrag(note, isGroup)
{
  /**
   * The note(s) being dragged.
   * @type Array
   */
  this.notes = [];

  // if ctrl is down, move all the notes of the same color
  if (isGroup)
  {
    for (const n in workspace.notes)
    {
      if (workspace.notes[n].bgColor.toString() == note.bgColor.toString())
      {
        this.notes.push( { 'id' : workspace.notes[n].id,
              'pos' : workspace.notes[n].getPosition() } );
      }
    }
  }
  else // single note move
    this.notes.push( { 'id' : note.id, 'pos' : note.getPosition() } );

  // set the border color of the notes that are being moved
  for (const n in this.notes)
  {
    const elt = get(this.notes[n].id);
    elt.style.border =  noteBorder + 'px #980000 solid';
  }
}
/**
 * Update the position of note(s) when the user moves the mouse.
 * @param {Mouse} md a reference to the parent mouse object
 */
SelectedObjectDrag.prototype.update = function(md)
{
  const offset = md.curPos.sub(md.downPos);
  for (const n in this.notes) {
    const elt = get(this.notes[n].id);
    const newPos = this.notes[n].pos.add(offset);
    elt.style.left = newPos.x + 'px';
    elt.style.top = newPos.y + 'px';
    if (BROWSER_MOZILLA == browser) {
      // this is a hack to force ffx to resize the content div
      const c = get('content');
      const d = document.createElement('span');
      c.appendChild(d);
      c.removeChild(d);
    }
  }
};
/**
 * When we drop a note, we need to reset the colors of borders and add
 * information to the undo stack.
 */
SelectedObjectDrag.prototype.deselect = function()
{
  // check to see if we moved the object(s). if we did, add
  // information to the undo stack.
  const md = workspace.mouse;
  const offset = md.curPos.sub(md.downPos);
  if (!offset.equals(new Point(0, 0)))
  {
    const f = {
      title : ((this.notes.length == 1) ? strings.HISTORY_MOVE_NOTE
                                        : strings.HISTORY_MOVE_NOTES),
      notes : this.notes,
      off : offset,
      undo : function()
      {
        for (const n in this.notes)
        {
          const elt = get(this.notes[n].id);
          const pos = this.notes[n].pos;
          elt.style.left = pos.x + 'px';
          elt.style.top = pos.y + 'px';
        }
      },
      redo : function()
      {
        for (const n in this.notes)
        {
          const elt = get(this.notes[n].id);
          const pos = this.notes[n].pos.add(this.off);
          elt.style.left = pos.x + 'px';
          elt.style.top = pos.y + 'px';
        }
      }
    };

    workspace.history.add(f);
  }
  // reset the border color to black
  for (const n in this.notes)
  {
    const elt = get(this.notes[n].id);
    elt.style.border =  noteBorder + 'px ' + noteBorderColor + ' solid';
  }
};


// inheritance might be useful here
/**
 * Create a new Resizing object.
 * @class A class that contains information about a note being resized.
 * @see Mouse#select
 * @param {Note} note a reference to the note being dragged
 * @param {dictionary} pnotePosRel which edges are being resized? It is
 * a dictionary from string -> boolean where the string is either
 * 'top, 'right', 'bottom' or 'left' and true means the edge is moving.
 * @constructor
 */
function SelectedObjectResize(note, pnotePosRel)
{
  /**
   * The note being resized
   * @type Note
   */
  this.note = note;
  /**
   * The original size of the note
   * @type Point
   */
  this.size = note.getSize();
  /**
   * The original position of the note
   * @type Point
   */
  this.pos = note.getPosition();
  /**
   * The edges being moved.
   * @type dictionary
   */
  this.edges = pnotePosRel;
}
/**
 * Update the size of the note when the user moves the mouse.
 * @param {Mouse} md a reference to the parent Mouse object
 */
SelectedObjectResize.prototype.update = function(md)
{
  const elt = get(this.note.id);

  // this depends on which edge they grabbed
  const minSize = 10;
  const offset = md.curPos.sub(md.downPos);
  if (this.edges['top'])
  {
    if (this.size.y - offset.y > minSize)
    {
      elt.style.top = (this.pos.y + offset.y) + 'px';
      elt.style.height = (this.size.y - offset.y) + 'px';
    }
  }
  else if (this.edges['bottom'])
    elt.style.height = Math.max(this.size.y + offset.y, minSize) + 'px';

  if (this.edges['left'])
  {
    if (this.size.x - offset.x > minSize)
    {
      elt.style.left = (this.pos.x + offset.x) + 'px';
      elt.style.width = (this.size.x - offset.x) + 'px';
    }
  }
  else if (this.edges['right'])
    elt.style.width = Math.max(this.size.x + offset.x, minSize) + 'px';

  if (this.note.parent.edit == this.note)
  {
    const edit = get(this.note.id + 'text');
    const pSize = this.note.getCorrectedSize();
    pSize.x -= 2*(noteBorder+notePadding+1);
    pSize.y -= 2*(noteBorder+notePadding+1) + 16;
    edit.style.height = Math.max(pSize.y, 10) + 'px';
    edit.style.width = Math.max(pSize.x, 10) + 'px';
  }

  this.note.updateSize();
};
/**
 * Add information to the undo stack when the user stops resizing.
 */
SelectedObjectResize.prototype.deselect = function()
{
  // add information to the undo stack if the item was resized
  const curSize = this.note.getSize();

  if (!this.size.equals(curSize))
  {
    const f = {
      title : strings.HISTORY_RESIZE_NOTE,
      usize : this.size,
      upos : this.pos,
      rsize : curSize,
      rpos : this.note.getPosition(),
      id : this.note.id,
      undo : function()
      {
        this.set(this.usize, this.upos);
      },
      redo : function()
      {
        this.set(this.rsize, this.rpos);
      },
      set : function(size, pos)
      {
        const elt = get(this.id);
        elt.style.top = pos.y + 'px';
        elt.style.left = pos.x + 'px';
        elt.style.height = size.y + 'px';
        elt.style.width = size.x + 'px';
        workspace.notes[this.id].updateSize();
      }
    };
    workspace.history.add(f);
  }
};


/**
 * @class A class that maintains the undo/redo stacks.
 */
export const History =
{
  /**
   * The number of items to keep in the undo stack.
   * @type int
   */
  maxSize : 40,
  /**
   * @type Array
   */
  undoStack : [], // each item in the array is an object
  /**
   * @type Array
   */
  redoStack : [], // with methods called undo and redo

  /**
   * Add an event to the undo stack.  This clears the redo stack.
   * @param {function} funcPtr a closure that when called will
   * undo the last action
   */
  add : function(funcPtr)
  {
    this.redoStack = [];
    this.undoStack.push(funcPtr);
    if (this.undoStack.length > this.maxSize)
      this.undoStack.shift();
    this.updateTitles();
  },
  /**
   * Undo the last action and move an item from the undo stack to the
   * redo stack.
   */
  undo : function()
  {
    if (this.undoStack.length > 0)
    {
      const f = this.undoStack.pop();
      this.redoStack.push(f);
      f.undo();
      this.updateTitles();
    }
  },
  /**
   * Redo the last undo action.  Moves the action back to the undo stack.
   */
  redo : function()
  {
    if (this.redoStack.length > 0)
    {
      const f = this.redoStack.pop();
      this.undoStack.push(f);
      f.redo();
      this.updateTitles();
    }
  },
  /**
   * Update the tool tips on the undo and redo icons.
   */
  updateTitles : function()
  {
    let elt = get('undoImg');
    if (0 == this.undoStack.length)
    {
      elt.title = strings.HISTORY_UNDO_EMPTY;
      elt.className = 'controlsDisabled';
      elt = get('saveImg');
      elt.className = 'controlsDisabled';
    }
    else
    {
      const tooltip = this.undoStack.length == 1 ? strings.HISTORY_UNDO_TOOLTIP
                                               : strings.HISTORY_UNDO_TOOLTIPS;
      elt.title = tooltip.replace("$1", this.undoStack[this.undoStack.length-1].title)
                         .replace("$2", this.undoStack.length);
      elt.className = 'controls';
      elt = get('saveImg');
      elt.className = 'controls';
    }
    elt = get('redoImg');
    if (0 == this.redoStack.length)
    {
      elt.title = strings.HISTORY_REDO_EMPTY;
      elt.className = 'controlsDisabled';
    }
    else
    {
      const tooltip = this.redoStack.length == 1 ? strings.HISTORY_UNDO_TOOLTIP
                                               : strings.HISTORY_UNDO_TOOLTIPS;
      elt.title = tooltip.replace("$1", this.redoStack[this.redoStack.length-1].title)
                         .replace("$2", this.redoStack.length);
      elt.className = 'controls';
    }
  }
};

//
/**
 * @class A generator class that returns the position of the next new note.
 */
export const NotePos =
{
  /**
   * @type int
   */
  x : 170,
  /**
   * @type int
   */
  y : 40,
  /**
   * Compute the position of a new note given the size of the note.
   * @param {int} w the width of the new note
   * @param {int} h the height of the new note
   * @type Point
   */
  nextPos : function(w, h) {
    const ret = new Point(this.x, this.y);

    this.x += 20;
    this.y += 20;
    const s = getPageSize();

    if (this.x + w > s.x || this.y + h > s.y) {
      this.x = 40;
      this.y = 40;
    }

    return ret;
  }
};


/**
 * @class A class that represents the workspace.  This includes maintaining
 * information about the notes and undo/redo information.
 */
export const workspace =
{
  /**
   * A dictionary of all the notes.
   * @type dictionary
   */
  notes : {},
  /**
   * When creating new notes, we sometimes need to assign a random name to
   * it.  The first random note is note0, the second is note1, etc.
   * @type int
   */
  nextNoteNum : 0,
  /**
   * Number of notes on the workspace.  We keep this as a separate variable
   * since there's no way to determine the size of a dictionary.
   * @type int
   */
  numNotes : 0,
  /**
   * The last time that we loaded this workspace (used to check for update
   * collision), in microseconds since the epoch.
   * @type int
   */
  loadedTime : 0,
  /**
   * Have we changed the workspace?
   * @type boolean
   */
  changed : false,
  /**
   * The note we are editing.
   * @type Note
   */
  edit : '',
  /**
   * The name of the workspace.
   * @type string
   */
  name : '',

  /**
   * @type History
   */
  history : History,
  /**
   * @type NotePos
   */
  notePos : NotePos,
  /**
   * @type Mouse
   */
  mouse : Mouse,
  /**
   * Mouse down position on the document.
   * @type Point
   */
  mouseDown : null,
  /**
   * Should keyboard shortcuts work?
   * @type boolean
   */
  shortcuts : true,
  /**
   * The id of the note on top.
   * @type string
   */
  topId: '',
  /**
   * How frequently we check for changes.
   */
  updateInterval: 1000*60*10,
  /**
   * The pending check for changes (see checkUpdated), and whether one is in
   * flight. There's only ever one of each.
   */
  pollTimer: 0,
  pollRunning: false,
  /**
   * The newest save the collision warning has shown, so closing the warning
   * doesn't bring it back for the same save.
   */
  warnedTime: 0,

  /**
   * Create a new note. Any of the parameter values may be left blank
   * and default values will be used.
   * @param {dictionary} note a dictionary with any of the following keys:
   * note['id'] = the name of the note<br />
   * note['xPos'] = the x position of the note<br />
   * note['yPos'] = the y position of the note<br />
   * note['height'] = the height of the note<br />
   * note['width'] = the width of the note<br />
   * note['bgcolor'] = the background color of the note (hex)<br />
   * note['zIndex'] = the stacking position of the note<br />
   * note['text'] = the text value of the note<br />
   */
  createNote : function(note, skipFilter) {
    if (!note)
      note = {};
    if (!('id' in note)) {
      note.id = "note" + this.nextNoteNum;

      // a new note is being made, save information to the undo stack
      const self = this;
      const f = {
        title : strings.HISTORY_CREATE_NOTE,
        nnn : this.nextNoteNum,
        id : note.id,
        pos : new Point(this.notePos.x, this.notePos.y),
        undo : function()
        {
          self.notes[this.id].destroy(); // don't add to history
          self.nextNoteNum = this.nnn;
        },
        redo : function()
        {
          self.createNote({'id': this.id, 'xPos' : this.pos.x,
              'yPos' : this.pos.y});
          self.nextNoteNum++;
        }
      };
      this.history.add(f);

      this.nextNoteNum++;
    }

    // don't create a layer if it already exists, just move it to the top
    if (get(note.id))
    {
      this.reZOrder(note.id);
      return note;
    }

    if (!('height' in note)) note.height = 150;
    if (!('width' in note)) note.width = 250;

    let pos;
    if (!('xPos' in note) || !('yPos' in note)) {
      pos = this.notePos.nextPos(note.width, note.height);
      const content = get('content');
      pos.x += parseInt(content.scrollLeft);
      pos.y += parseInt(content.scrollTop);
    }
    if (!('xPos' in note)) note.xPos = pos.x;
    if (!('yPos' in note)) note.yPos = pos.y;

    if (!('bgcolor' in note)) note.bgcolor = bgColors[0].toString();

    if (!('text' in note)) note.text = '';

    // disable editing of a different note
    this.editOff();

    const newDiv = document.createElement('div');
    newDiv.className = 'note';
    newDiv.id = note.id;
    // work around a safari bug
    if (BROWSER_SAFARI == browser) {
      newDiv.style.opacity = '0.99';
      newDiv.style.overflow = 'hidden';
    }
    get('content').appendChild(newDiv);

    //document.body.innerHTML += newDiv;
    const elt = get(note.id);
    elt.style.backgroundColor = note.bgcolor;
    elt.style.width = note.width + 'px';
    elt.style.height = note.height + 'px';
    elt.style.left = note.xPos + 'px';
    elt.style.top = note.yPos + 'px';
    elt.style.padding = notePadding + 'px';
    elt.style.position = 'absolute';
    elt.style.border = noteBorder + 'px ' + noteBorderColor + ' solid';

    if (!('zIndex' in note)) {
      this.reZOrder();
      elt.style.zIndex = this.numNotes + 1;
      this.topId = note.id;
    } else {
      elt.style.zIndex = note.zIndex;
      const topElt = get(this.topId);
      if (topElt) {
        if (parseInt(note.zIndex) > parseInt(topElt.style.zIndex)) {
          this.topId = note.id;
        }
      } else {
        this.topId = note.id;
      }
    }
    this.numNotes++;
    const nNote = new Note(elt, this, note.text);
    this.notes[nNote.id] = nNote;

    newDiv.addEventListener('mouseover', nNote.mouseOver.bind(nNote));
    newDiv.addEventListener('mouseout', nNote.mouseOut.bind(nNote));
    newDiv.addEventListener('mousedown', nNote.mouseDown.bind(nNote));
    newDiv.addEventListener('mouseup', nNote.mouseUp.bind(nNote));
    newDiv.addEventListener('mousemove', nNote.mouseMove.bind(nNote));
    newDiv.addEventListener('dblclick', nNote.mouseDblClick.bind(nNote));
    // only the text of the note being edited can be selected
    newDiv.addEventListener('selectstart', function(ev) {
      if (workspace.edit != nNote)
        ev.preventDefault();
    });
    newDiv.title = strings.NOTE_TOOLTIP;

    if (!skipFilter) {
      this.filter('');
    } else {
      // this normally gets called by filter
      workspace.updateMiniBox();
    }

    return nNote;
  },
  /**
   * Mouse up action on the workspace.
   */
  mouseUp : function() {
    if (this.mouse.selObj) {
      this.mouse.selObj.deselect();
      delete this.mouse.selObj;
    }
    // stop dragging the document
    this.mouseDown = null;
  },

  docMove : function(x, y) {
    if (this.mouseDown) {
      const offX = this.mouseDown.x - x;
      const offY = this.mouseDown.y - y;
      const content = get('content');
      content.scrollTop += offY;
      content.scrollLeft += offX;
      this.mouseDown.x = x;
      this.mouseDown.y = y;
    }
  },

  /**
   * If we are editing any note, stop editing.
   */
  editOff : function()
  {
    if (this.edit)
    {
      const textbox = get(this.edit.id + 'text');
      if (BROWSER_SAFARI != browser) {
        textbox.parentNode.style.overflow = 'auto';
      }

      // check to see if the text changed.  add to the
      // undo stack if it did.
      if (textbox.value != this.edit.text)
      {
        const f = {
          title : strings.HISTORY_EDIT_NOTE,
          utext : this.edit.text,
          rtext : textbox.value,
          note : this.edit,
          undo : function()
          {
            this.note.setText(this.utext);
          },
          redo : function()
          {
            this.note.setText(this.rtext);
          }
        };
        this.history.add(f);
      }

      this.edit.setText(textbox.value);
      this.edit = '';
    }
  },
  /**
   * Resort the notes and place topNoteID in front.
   * @param {string} topNoteID the name of the note to bring to the front.
   */
  reZOrder : function(topNoteID) {
    if (this.notes) {
      // it's not possible to sort an associative array
      // so we copy it into a regular array first
      const nArray = [];
      let i = 0;
      for (const nid in this.notes) {
        nArray[i] = this.notes[nid];
        ++i;
      }

      nArray.sort(cmpNotesZ);

      // set zIndex based on order
      let found = 0;
      for (i = 0; i < nArray.length; ++i) {
        if (nArray[i].id == topNoteID) {
          found = 1;
          get(nArray[i].id).style.zIndex = this.numNotes;
          this.topId = nArray[i].id;
        }
        else
          get(nArray[i].id).style.zIndex = i + 1 - found;
      }
    }
  },

  /**
   * Set the name of the workspace.  NOTE: this changes where notes get
   * saved.
   * @param {string} n the new name
   */
  setName : function(n)
  {
    this.name = n;
    get('wsname').textContent = this.name;
    // The page's <title> has the name as the URL had it, which for links
    // from the client is in myescape() form.
    document.title = this.name;
  },

  /**
   * Filter the visible notes (kind of like a search).  Notes that match
   * the regular expression are moved to the front while other notes are
   * disabled and become mostly transparent.
   * @param {string} text the regular expression to filter by
   */
  filter : function(text) {
    let shouldHide;
    if (text.trim().indexOf(strings.COLOR + ":") == 0) {
      const color = text.trim().substring(strings.COLOR.length + 1);
      shouldHide = function(note) {
        return (colorMap[note.bgColor.toString()] == color);
      };
    }
    else {
      const reg = new RegExp(text, 'i');
      shouldHide = function(note) {
        return reg.test(note.text);
      };
    }

    for (const n in this.notes) {
      const note = this.notes[n];
      const elt = get(note.id);
      if (shouldHide(note)) {
        elt.className = 'note';
        if (BROWSER_SAFARI == browser) elt.style.opacity = '0.99';
        note.enable();
        get('m' + note.id).className = 'mininote';
      } else {
        elt.className = 'noteHide';
        elt.style.zIndex = 0;
        if (BROWSER_SAFARI == browser) elt.style.opacity = '0.2';
        note.disable();
        get('m' + note.id).className = 'mininote noteHide';
      }
    }
    get('textfilter').value = text;
    this.reZOrder();
    this.updateMiniBox();
  },
  /**
   * Update the background color and tool tips of the mini notes.
   */
  updateMiniBox : function() {
    const mini = get('mini');
    let vNotes = 0;
    let total = 0;

    for (const n in this.notes) {
      ++total;
      if (get('m' + n).className.indexOf("noteHide") == -1)
        ++vNotes;
    }

    // Set the width of the mini div.
    // After the first 9 notes, we add space for additional notes
    // 10 is the width of a note.
    const miniNoteWidth = 10;
    const diff = parseInt(Math.max(0, total - 9) / 2) * miniNoteWidth;
    mini.style.width = (diff + miniWidth) + 'px';

    if (0 == total)
      mini.title = strings.MINI_NO_NOTES;
    else if (vNotes == total)
      mini.title = strings.MINI_SHOWING_ALL.replace("$1", total);
    else if (0 == vNotes)
      mini.title = strings.MINI_HIDING_ALL.replace("$1", total);
    else {
      mini.title = strings.MINI_SHOWING_PARTIAL
                          .replace("$1", vNotes)
                          .replace("$2", total);
    }
  },
  /**
   * Create the "load old notes" note.
   * @param {int} offset how many saves do we want to go back?
   */
  loadlist : function(offset) {
    if (!offset)
      offset = 0;

    showDialog('historyDialog', 'history', true,
               [paragraph(strings.LOADVERSIONS_INIT), dialogButtons([closeButton()])]);

    const failed = function() {
      workspace.loadlistFailed(offset);
    };
    fetch('getdates.py?name=' + escape(myescape(this.name))
          + '&offset=' + offset + '&epoch=1').then(function(response) {
      if (200 != response.status) {
        db('getdates: ' + response.status);
        failed();
        return;
      }
      return response.text().then(function(text) {
        workspace.loadlistCallback(text, offset);
      });
    }, failed);
  },

  loadlistFailed : function(offset) {
    // the user may have closed the history already
    if (!dialogOpen('historyDialog'))
      return;
    const retry = document.createElement('button');
    retry.type = 'button';
    retry.textContent = strings.LOADVERSIONS_RETRY;
    retry.addEventListener('click', function() { workspace.loadlist(offset); });
    showDialog('historyDialog', 'history', true,
               [paragraph(strings.LOADVERSIONS_ERROR), dialogButtons([retry, closeButton()])]);
  },

  loadlistCallback : function(text, offset) {
    if (!dialogOpen('historyDialog'))
      return;
    const loadTimes = text.split('|');
    db('resp: ' + text.trim());
    db('resplen: ' + text.trim().length);
    if (text.trim() == '') {
      showDialog('historyDialog', 'history', true,
                 [paragraph(strings.LOADVERSIONS_NONE), dialogButtons([closeButton()])]);
      return;
    }

    const form = document.createElement('form');
    form.method = 'GET';
    form.action = 'load.py';
    const name = document.createElement('input');
    name.type = 'hidden';
    name.name = 'name';
    name.value = myescape(this.name);
    const select = document.createElement('select');
    select.className = 'loadfrm';
    select.name = 'time';
    for (let i = 0; i < loadTimes.length && i < numDates; i++) {
      select.add(new Option(String(new MyDate(loadTimes[i])), loadTimes[i].trim()));
    }
    const submit = document.createElement('input');
    submit.type = 'submit';
    submit.value = 'load';

    const links = document.createElement('div');
    links.className = 'pages';
    const pageLink = function(text, newOffset) {
      const a = document.createElement('a');
      a.className = 'fakelink';
      a.textContent = text;
      a.addEventListener('click', function() { workspace.loadlist(newOffset); });
      return a;
    };
    if (loadTimes.length > numDates)
    {
      links.append('\u00ab ', pageLink(strings.LOADVERSIONS_OLDER, offset + numDates), ' ');
      if (offset > 0)
        links.append('| ');
    }
    if (offset > 0) {
      links.append(pageLink(strings.LOADVERSIONS_NEWER, offset - numDates), ' \u00bb');
    }
    links.append(document.createElement('br'), strings.LOADVERSIONS_DATE_NOTE);

    form.append(name, select, ' ', submit, links);
    showDialog('historyDialog', 'history', true,
               [paragraph(strings.LOADVERSIONS_ABOUT), form, dialogButtons([closeButton()])]);
  },

  /**
   * Check for changes again in updateInterval, replacing any pending check.
   */
  schedulePoll : function() {
    window.clearTimeout(this.pollTimer);
    this.pollTimer = window.setTimeout(function() { workspace.checkUpdated(); },
                                       this.updateInterval);
  },

  /**
   * Is the collision or conflict warning shown?  Checks for changes stop
   * until it's closed.
   */
  warningShown : function() {
    return dialogOpen('collisionDialog') || dialogOpen('conflictDialog');
  },

  /**
   * Checks to see if the notes have changed since the last time we loaded
   * the notes.  Hidden tabs skip the check, and check when shown again (see
   * init).
   */
  checkUpdated : function() {
    window.clearTimeout(this.pollTimer);
    this.pollTimer = 0;
    if (this.pollRunning || this.warningShown() || 'hidden' == document.visibilityState)
      return;
    this.pollRunning = true;
    const failed = function(status) {
      db('getrecent: ' + status);
      workspace.pollRunning = false;
      workspace.schedulePoll();
    };
    fetch('getrecent.py?name=' + escape(myescape(this.name))
          + '&epoch=1').then(function(response) {
      if (200 != response.status) {
        failed(response.status);
        return;
      }
      return response.text().then(function(text) {
        db('check updated');
        workspace.pollRunning = false;
        workspace.checkUpdatedCallback(text);
      });
    }, function() { failed(0); });
  },

  checkUpdatedCallback : function(text) {
    // Times are microseconds since the epoch, exact in a JS number until 2255.
    const loadTime = Number(text.trim());
    if (this.warningShown()) {
      // shown while the check was in flight; closing it checks again
    } else if (loadTime > Math.max(this.loadedTime, this.warnedTime)) {
      this.warnedTime = loadTime;
      this.showCollision();
    } else {
      db('no changes');
      this.schedulePoll();
    }
  },

  /**
   * Say that someone else saved the workspace, in a banner that doesn't stop
   * the user.  Checks for changes wait until it's closed.
   */
  showCollision : function() {
    const open = document.createElement('a');
    open.href = escape(escape(myescape(this.name)));
    open.target = '_blank';
    open.textContent = strings.COLLISION_OPEN;
    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'closeDialog';
    close.textContent = '\u00d7';
    close.title = strings.DIALOG_CLOSE;
    close.setAttribute('aria-label', strings.DIALOG_CLOSE);
    close.addEventListener('click', function() { get('collisionDialog').close(); });
    showDialog('collisionDialog', 'collision', false,
               [close, strings.COLLISION_WARNING + ' ', open],
               function() { workspace.schedulePoll(); });
  },

  /**
   * Someone else saved the workspace (at time, in microseconds since the
   * epoch) after this page loaded it, so the save didn't happen.  Let the
   * user choose what to do.  Keep editing (or Esc) closes the dialog, and
   * the next save conflicts again, until the user saves anyway.
   */
  showConflict : function(time) {
    // this replaces the collision warning, which needn't come back for
    // this save
    if (dialogOpen('collisionDialog'))
      get('collisionDialog').close();
    this.warnedTime = Math.max(this.warnedTime, time);

    const self = this;
    const newer = document.createElement('a');
    newer.href = 'load.py?name=' + escape(myescape(this.name)) + '&time=' + time;
    newer.target = '_blank';
    newer.textContent = strings.CONFLICT_OPEN_NEWER;
    const saveAnyway = dialogButton(strings.CONFLICT_SAVE_ANYWAY, function() {
      // Base the save on the version it replaces.  If yet another save
      // landed since, it conflicts again.
      self.loadedTime = time;
      get('conflictDialog').close();
      self.save();
    });
    const reload = dialogButton(strings.CONFLICT_RELOAD, function() {
      self.changed = false;
      location.href = escape(escape(myescape(self.name)));
    });
    const keepEditing = dialogButton(strings.CONFLICT_KEEP_EDITING, function() {
      get('conflictDialog').close();
    });
    showDialog('conflictDialog', 'conflict', true,
               [paragraph(strings.CONFLICT_WARNING), paragraph(newer),
                dialogButtons([saveAnyway, reload]), dialogButtons([keepEditing])],
               function() { workspace.schedulePoll(); });
    saveAnyway.focus();
  },

  /**
   * Generate the xml representation of the workspace (used when saving).
   * @type string
   */
  toString : function()
  {
    // Workspace names are stored in myescape() form (see objects.js).
    let ret = "<workspace name='" + myescape(this.name) + "'";
    ret += " nextNoteNum='" + this.nextNoteNum + "'";
    ret += ">\n";
    for (const nid in this.notes)
      ret += this.notes[nid].toXML() + "\n";
    return ret + "</workspace>"
  },
  /**
   * Get the next (or previous) note relative to the top note.
   * @param {int} diff The offset from the top note (positive mean
   * below and negative mean up from the bottom note).
   */
  selectNote : function(diff)
  {
    let max = -1;
    let maxNote;
    const noteArr = [];
    // determine which note is on top
    for (const n in this.notes)
    {
      const cur = parseInt(get(this.notes[n].id).style.zIndex);
      if (cur > max)
      {
        max = cur;
        maxNote = noteArr.length;
      }
      noteArr.push(this.notes[n]);
    }
    noteArr[ (maxNote+diff+noteArr.length) % noteArr.length ].select();
  },
  /**
   * Save the workspace.
   */
  save : function()
  {
    // there have to be changes to the workspace before saving is enabled
    const s = get('saveImg');
    if (s.className == 'controlsDisabled')
      return;
    // A second save sent before the first answers would carry the same
    // loadedTime, so it would conflict with the first.
    if (s.dataset.state == 'saving')
      return;

    this.setSaveState('saving');
    this.editOff();

    const failed = function(status) {
      db(status);
      alert(strings.SAVE_STATUS_ERROR);
      workspace.setSaveState('');
    };
    fetch('save.py?epoch=1&loaded=' + this.loadedTime,
          {method: 'POST', body: workspace.toString()}).then(function(response) {
      if (409 == response.status) {
        return response.text().then(function(text) {
          workspace.saveCallback(text);
        });
      }
      if (200 != response.status) {
        failed(response.status);
        return;
      }
      return response.text().then(function(text) {
        workspace.saveCallback(text);
      });
    }, function() { failed(0); });
  },
  saveCallback : function(text) {
    const doc = new DOMParser().parseFromString(text, 'text/xml');
    const status = doc.getElementsByTagName('status')[0];
    db("response: " + text);
    if (status && 'ok' == status.getAttribute('value')) {
      this.changed = false;
      get('saveImg').className = 'controlsDisabled';
      this.loadedTime = Number(status.getAttribute('update'));
      this.setSaveState('saved');
      return;
    } else if (status && 'conflict' == status.getAttribute('value')) {
      this.showConflict(Number(status.getAttribute('update')));
    } else {
      alert(strings.SAVE_SERVER_ERROR.replace("$1", adminEmail));
    }
    this.setSaveState('');
  },
  /**
   * The save button's icon: 'saving' (the arrow rises), 'saved' (a check,
   * for a second) or '' (see style.css).
   */
  setSaveState : function(state) {
    window.clearTimeout(this.savedTimer);
    get('saveImg').dataset.state = state;
    if ('saved' == state) {
      this.savedTimer = window.setTimeout(function() { workspace.setSaveState(''); }, 1000);
    }
  },
  savedTimer : 0
};

///
/// global methods
///

/**
 * Initialize the workspace.
 */
export function init()
{
  reportErrors(window);
  if (debugOn) {
    const debug = document.createElement('textarea');
    debug.id = 'debug';
    debug.cols = 50;
    debug.rows = 10;
    debug.style.cssText = 'position:absolute;top:26px;right:10px;';
    const clear = document.createElement('input');
    clear.type = 'button';
    clear.value = 'clear';
    clear.addEventListener('click', function() { debug.value = ''; });
    const form = document.createElement('form');
    form.append(debug, clear);
    get('db').appendChild(form);
  }


  const toolbar = get('toolbar');
  toolbar.addEventListener('mousedown', stopPropagation);
  get('newImg').addEventListener('click', function() { workspace.createNote(); });
  get('saveImg').addEventListener('click', function() { workspace.save(); });
  get('reloadImg').addEventListener('click', function() { workspace.loadlist(); });
  get('undoImg').addEventListener('click', function() { workspace.history.undo(); });
  get('redoImg').addEventListener('click', function() { workspace.history.redo(); });
  const textfilter = get('textfilter');
  textfilter.addEventListener('change', function() { workspace.filter(textfilter.value); });
  textfilter.addEventListener('keydown', function(ev) {
    if ('Enter' == ev.key)
      workspace.filter(textfilter.value);
    ev.stopPropagation();
  });
  if (BROWSER_SAFARI == browser) {
    toolbar.style.width = '100%';
  }

  workspace.mouse.curPos = new Point();
  document.addEventListener('mousemove', docMouseMove);
  document.addEventListener('keydown', docKeyDown);
  get('content').addEventListener('mousedown', docMouseDown);
  document.addEventListener('mouseup', docMouseUp);

  /**
   * When the user navigates away, if there are changes, give the user a
   * chance to cancel.
   */
  window.addEventListener('beforeunload', function(ev) {
    const warning = winBeforeUnload();
    if (warning) {
      ev.preventDefault();
      ev.returnValue = warning;
    }
  });

  // set tooltips to localized text
  get('newImg').title = strings.ICON_NEW_NOTE;
  get('saveImg').title = strings.ICON_SAVE;
  get('reloadImg').title = strings.ICON_LOAD;
  get('undoImg').title = strings.HISTORY_UNDO_EMPTY;
  get('redoImg').title = strings.HISTORY_REDO_EMPTY;
  get('textfilter').title = strings.FILTER_TITLE;
  get('mini').title = strings.MINI_NO_NOTES;
  get('rsslink').title = strings.RSS_LINK_TITLE;

  // periodically check for updates (every 10 minutes)
  workspace.schedulePoll();
  // A hidden tab skips its check; check when it's shown again.
  document.addEventListener('visibilitychange', function() {
    if ('visible' == document.visibilityState && !workspace.pollTimer)
      workspace.checkUpdated();
  });
}

/**
 * Show the dialog with this id, modal or not, with new contents.  The first
 * time, make it, with onClose (if given) for its close event.  The client's
 * own messages are dialogs rather than notes, so they aren't saved and their
 * ids don't clash with the notes older versions saved for them (load,
 * updated and conflict).
 * @return the dialog
 */
function showDialog(id, className, modal, contents, onClose) {
  let dialog = get(id);
  if (!dialog) {
    dialog = document.createElement('dialog');
    dialog.id = id;
    dialog.className = 'message ' + className;
    // Clicks and keys in a dialog aren't for the notes underneath.
    dialog.addEventListener('mousedown', stopPropagation);
    dialog.addEventListener('keydown', stopPropagation);
    if (onClose)
      dialog.addEventListener('close', onClose);
    document.body.appendChild(dialog);
  }
  dialog.replaceChildren(...contents);
  if (!dialog.open) {
    if (modal) {
      dialog.showModal();
    } else {
      // show() focuses the dialog; leave the focus where the user is typing
      const active = document.activeElement;
      dialog.show();
      if (active)
        active.focus();
    }
  }
  return dialog;
}

function dialogOpen(id) {
  const dialog = get(id);
  return Boolean(dialog && dialog.open);
}

function paragraph(content) {
  const p = document.createElement('p');
  p.append(content);
  return p;
}

function dialogButton(text, onclick) {
  const button = document.createElement('button');
  button.type = 'button';
  button.textContent = text;
  button.addEventListener('click', onclick);
  return button;
}

function closeButton() {
  return dialogButton(strings.DIALOG_CLOSE, function(ev) {
    ev.target.closest('dialog').close();
  });
}

function dialogButtons(buttons) {
  const div = document.createElement('div');
  div.className = 'buttons';
  div.append(...buttons);
  return div;
}

function stopPropagation(e) {
  e.stopPropagation();
}

function preventDefault(e) {
  e.preventDefault();
}

/**
 * Convert characters 160-255 to character references, which is how note
 * text is stored (this was for Safari compatibility).
 * @param {string} str
 * @type string
 */
export function charRefs(str) {
  return str.replace(/[\u00a0-\u00ff]/g, function(c) {
    return '&#' + c.charCodeAt(0) + ';';
  });
}

/**
 * Decode the character references in a note's text, as the HTML parser did
 * when the edit box was built from a string (setText stores characters
 * 160-255 as &#nnn;). Nothing else is parsed: a textarea's contents are text.
 * @param {string} text
 * @type string
 */
function decodeCharRefs(text) {
  const textarea = document.createElement('textarea');
  textarea.innerHTML = text.replace(/</g, '&lt;');
  return textarea.value;
}
// absolute mouse positions; modified from:
// http://www.web-wise-wizard.com/javascript-tutorials/javascript-mouse-event-handlers.html
function docMouseMove(e) {
  workspace.mouse.update(e.pageX, e.pageY);
  workspace.docMove(e.pageX, e.pageY);
}

/**
 * If the user clicks on the background, turn off note editing and start dragging
 */
function docMouseDown(e) {
  workspace.editOff();

  // force a position update
  docMouseMove(e);
  const curPos = workspace.mouse.curPos.copy();
  // get rel pos
  // make sure we're not on a scrollbar
  const content = get('content');
  if (curPos.x < content.clientWidth && curPos.y < content.clientHeight) {
    workspace.mouseDown = workspace.mouse.curPos.copy();
  }
}

/**
 * @see workspace#mouseUp
 */
function docMouseUp(e) {
  workspace.mouseUp(e);
}

function docKeyDown(ev) {
  // the keydown event only seems to be a document event so we
  // can't put the event on the div layer.  Instead, events
  // need to be passed to the note that is being hovered over.
  const key = String.fromCharCode(ev.keyCode);
  let n;
  if (workspace.shortcuts && !workspace.edit) {
    // blah, I should turn this into a map
    if ('P' == key && ev.altKey) {
      const note = workspace.createNote();
      note.mouseDblClick();
    } else if ('S' == key && ev.altKey) {
      workspace.save();
    } else if (37 == ev.keyCode) { // left
      n = workspace.notes[workspace.topId];
      if (n) n.move(new Point(-8, 0));
    } else if (38 == ev.keyCode) { // up
      n = workspace.notes[workspace.topId];
      if (n) n.move(new Point(0, -8));
    } else if (39 == ev.keyCode) { // right
      n = workspace.notes[workspace.topId];
      if (n) n.move(new Point(8, 0));
    } else if (40 == ev.keyCode) { // down
      n = workspace.notes[workspace.topId];
      if (n) n.move(new Point(0, 8));
    } else if ('O' == key && ev.altKey) {
      n = workspace.notes[workspace.topId];
      n.mouseDblClick();
    } else if ('N' == key) {
      if (ev.altKey) {
        workspace.createNote();
      } else {
        workspace.selectNote(1);
      }
    } else if ('P' == key) {
      workspace.selectNote(-1);
    } else if ('B' == key) {
      workspace.createNote({
      'text': strings.BOOKMARKLET_TEXT
        + "\n<a href=\"javascript:var d=document;var e=encodeURIComponent;if(d.getSelection)txt=d.getSelection();if(d.selection)txt=d.selection.createRange().text;location.href='" + baseURI + "load.py?name="
        + escape(escape(myescape(workspace.name))) + "&via='+e(location.href)+'&nn='+e(txt);\">"
        + strings.BOOKMARKLET_NAME + "</a>"});
    } else { // forward to note
      n = workspace.notes[workspace.topId];
      n.keyDown(ev);
    }
  } else if (workspace.shortcuts) { // in edit mode
    if (27 == ev.keyCode) { // Esc
      workspace.editOff();
    } else if ('D' == key && ev.altKey) {
      n = workspace.notes[workspace.topId];
      n.destroy(true);
      ev.preventDefault();
    } else { // forward to note
      n = workspace.notes[workspace.topId];
      n.keyDown(ev);
    }
  }
}

// The deployed version, from the ?v= the page puts on the module URLs.
const version = new URL(import.meta.url).searchParams.get('v') || '';
// The most errors one page reports, so a polling loop that keeps failing
// doesn't send a report every few seconds.
const maxErrorReports = 5;

/**
 * Report uncaught errors to the server (jserror in server.go), each message
 * once. Reports leave out the page's URL, which has the workspace name (the
 * request log has it as the Referer), and errors from other origins, such as
 * extensions.
 */
export function reportErrors(win) {
  const sent = new Set();
  const page = win.location.href.split('#')[0];
  const scrub = s => String(s || '').split(page).join('page').slice(0, 4000);
  const send = function(error, message, source, line, col) {
    if (error instanceof Error)
      message = String(error);  // "TypeError: x", without "Uncaught "
    else if (!message)
      message = String(error);
    if (source && !source.startsWith(win.location.origin))
      return;
    if (!source && 'Script error.' == message)  // cross-origin, no details
      return;
    message = scrub(message);
    if (sent.has(message) || sent.size >= maxErrorReports)
      return;
    sent.add(message);
    navigator.sendBeacon('jserror', JSON.stringify({
      message, source: scrub(source), line: line || 0, col: col || 0,
      stack: scrub(error && error.stack), version}));
  };
  win.addEventListener('error', function(ev) {
    send(ev.error, ev.message, ev.filename, ev.lineno, ev.colno);
  });
  win.addEventListener('unhandledrejection', function(ev) {
    send(ev.reason, '', '', 0, 0);
  });
}

/**
 * Give the user a chance to save workspace.
 */
export function winBeforeUnload() {
  if (workspace.changed && workspace.name != "Sample Workspace") {
    return strings.UNLOAD_WARNING;
  }
}

/**
 * Get the size of the browser.
 * @type Point
 */
function getPageSize() {
  return new Point(window.innerWidth, window.innerHeight);
}

