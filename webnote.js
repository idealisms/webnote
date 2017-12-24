/**
 * @fileoverview The Webnote specific classes.<br /><br />
 *
 * The two main classes are {@link workspace} and {@link Note}.
 *
 * {@link workspace} represents the workspace as a whole.  It contains
 * refernces to all the notes and methods for operations such as saving
 * the notes or undo-ing/redo-ing an action.
 */

// global variables
// user options
var debugOn = 0;
var notePadding = 5;
var noteBorder = 3;
var noteBorderColor = '#000';
var miniWidth = 51;
var exposeSize = "70";

var adminEmail, baseURI, numDates;

// TODO: make it easier for users to make custom color maps
var colorMap = {
  '#ffff30': strings.COLOR_YELLOW,
  '#8facff': strings.COLOR_BLUE,
  '#7fff70': strings.COLOR_GREEN,
  '#ff6060': strings.COLOR_RED,
  '#ffb440': strings.COLOR_ORANGE,
  '#ff6bef': strings.COLOR_PURPLE,
  '#ffffff': strings.COLOR_WHITE,
  '#b0b0b0': strings.COLOR_GREY
};
var bgColors = [];
for (var c in colorMap)
{
  bgColors.push(c);
}

// other global variables, you shouldn't change these
var BROWSER_IE_6 = 0;
var BROWSER_IE_5 = 1;
var BROWSER_SAFARI = 2;
var BROWSER_MOZILLA = 3;
var BROWSER_OPERA = 4;
var browser;

(function () { // closure away userAgent
  var userAgent = navigator.userAgent.toLowerCase();
  if (userAgent.indexOf('opera') != -1) { // opera contains msie and opera
    browser = BROWSER_OPERA;
  } else if (userAgent.indexOf('webkit') != -1) {
    browser = BROWSER_SAFARI;
  } else if (userAgent.indexOf('msie') != -1) {
    if (userAgent.indexOf('5.5') != -1 || userAgent.indexOf('5.0') != -1) {
      browser = BROWSER_IE_5;
    } else {
      browser = BROWSER_IE_6;
    }
  } else {
    browser = BROWSER_MOZILLA;
  }
})();

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
function Note(note, p, text) {
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
  var dotElt = document.createElement('div');
  dotElt.id = 'm' + this.id;
  dotElt.onmousedown = hitch(this, this.miniMouseDown);
  get('mini').appendChild(dotElt);

  var dot = get('m' + this.id);
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
  ev = ev || window.event;
  var target = ev.target || ev.srcElement;
  if (!this.selectable)
    return false;

  // alt + rt click == send to back
  // 2 is rt click in both IE and Mozilla
  if (ev.altKey && 2 == ev.button) {
    this.sendToBack();
    return false;
  }

  var lbutton = false;
  if (BROWSER_IE_6 == browser || BROWSER_IE_5 == browser) // IE button mapping (1, 2, 4)
    lbutton = 1 == ev.button;
  else // mozilla/safari 2 and w3c's button mapping (0, 1, 2)
    lbutton = 0 == ev.button;

  // only the left button should have an event
  // likewise we can disable actions by using meta or ctrl+alt
  if (!lbutton || ev.metaKey || (ev.ctrlKey && ev.altKey))
    return true;

  // Determine if we're clicking on the note or a scrollbar
  var pos = findRelativeMousePos(ev);
  var size = this.getCorrectedSize();

  if (!target.clientWidth || // no clientWidth if you click on a scrollbar in FF1.0
      (pos.x > target.clientWidth + noteBorder
       && pos.x < size.x - noteBorder) ||
      (pos.y > target.clientHeight + noteBorder
       && pos.y < size.y - noteBorder)) {
    cancelBubble(ev);
    return;
  }

  this.parent.mouse.select(this, ev);
  cancelBubble(ev);
  return false;
}

/**
 * User clicked on one of the mini notes at the top of the page.
 */
Note.prototype.miniMouseDown = function(ev)
{
  ev = ev || window.event;
  // bring all notes of the same color to the front
  if (ev.ctrlKey) {
    for (var n in this.parent.notes) {
      var note = this.parent.notes[n];
      if (note != this && note.bgColor.toString() == this.bgColor.toString())
        note.select();
    }
  } else if (ev.shiftKey) {
    workspace.filter(strings.COLOR + ":" + colorMap[this.bgColor.toString()]);
  }
  this.select();

  // bring it back if it's off screen
  var size = this.getCorrectedSize();
  var pos = this.getPosition();
  var elt = get(this.id);
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
  var idx = parseInt(String.fromCharCode(ev.keyCode)) - 1;
  if (idx >= 0 && idx < bgColors.length)
    this.setColor(bgColors[idx]);
}
/**
 * Moving the mouse while over a note (changes cursor).
 */
Note.prototype.mouseMove = function(ev)
{
  ev = ev || window.event;
  var elt = get(this.id);
  if (!this.selectable)
  {
    elt.style.cursor = 'auto';
    return;
  }
  if (this.parent.mouse.selObj)
    return;

  var top = false;
  var left = false;
  var right = false;
  var bottom = false;
  var resize_cursor = '';
  var size = this.getCorrectedSize();
  var relPos = findRelativeMousePos(ev);

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
  var elt = get(this.id);
  elt.style.background = (new Color(this.bgColor.toString())).hsvadj(0, 0, -0.1);
  this.parent.mouse.noteOver = this;
}
/**
 * Mouse leaves a note (restore original background color).
 */
Note.prototype.mouseOut = function() {
  var elt = get(this.id);
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
  var pSize = this.getCorrectedSize();
  pSize.x -= 2 * (noteBorder + notePadding + 1);
  pSize.y -= 2 * (noteBorder + notePadding + 1) + 20;
  var html = "<div style='text-align:right;margin: 0 2px 1px 0;'>"

  // color swatches here
  for (var c in bgColors) {
    var tooltip = strings.COLOR_SWATCH_TOOLTIP.replace('$1', parseInt(c, 10) + 1);
    html += "<div style='width: 12px;height:12px;font-size:1px;float:left;background: "
          + bgColors[c] + ";border: 1px #000 solid; margin:0 1px 1px 0;cursor:auto;' "
          + "onmousedown='workspace.notes." + this.id
          + ".setColor(\"" + bgColors[c] + "\");event.cancelBubble=true;' title='"
          + tooltip + "'></div>";
  }

  html += "<img onclick='workspace.notes." + this.id
        + ".destroy(true);' src='images/close.gif' alt='" + strings.CLOSE_ICON_ALT + "'"
        + " title='" + strings.CLOSE_ICON_TOOLTIP + "'"
        + " style='cursor:auto;border:0;height:12px;width:12px;' />"
        + "</div><textarea wrap='virtual' id='"
        + this.id + "text' style='width:" + pSize.x
        + "px;height:" + pSize.y + "px' onmousedown='event.cancelBubble=true;' ondblclick='event.cancelBubble=true;'>"
        + this.text.replace(/>/g, '&gt;').replace(/</g, '&lt;') + '</textarea>';
  var elt = get(this.id);
  elt.innerHTML = html;
  elt.title = '';
  elt.onselectstart = null;
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

  var elt = get(this.id);

  // save undo information
  if (addToHistory)
  {
    var pos = this.getPosition();
    var ws = this.parent;
    var f = {
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
  var ret = new Point(0, 0);
  var elt = get(this.id);
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
  var ret = this.getSize();

  if (BROWSER_IE_5 != browser) {
    var offset = 2*(noteBorder+notePadding);
    ret.x += offset;
    ret.y += offset;
  }
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
  var newColor = new Color(hex);

  if (!ignoreHistory)
  {
    var f = {
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

  var elt = get(this.id);
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
  var sCopy = this.text.replace(/\n/g,"<br />\n");
  sCopy = sCopy.replace(/\\<br \/>\n/g,"\n");

  // Turn URLs into links
  try {
    sCopy = sCopy.replace(/\s((http|ftp)(:\/\/[-a-zA-Z0-9%.~:_\/]+[a-zA-Z\/])([?&]([-a-zA-Z0-9%.~:_]+)=([-a-zA-Z0-9%.~:_])+)*(#([-a-zA-Z0-9%.~:_]+)?)?)/g,
                          '<a href="$1">$1</a>');
  } catch (e) {}
  var lines = sCopy.split('\n');
  for (var i = 0; i < lines.length; i++) {
    if (lines[i].length > 0) {
      // handle headings
      switch(lines[i].charAt(0)) {
        case '!': // headings
          var c = 0;
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
  var ret = "<note noteid='" + this.id + "'";
  ret += " bgcolor='" + this.bgColor + "'";
  ret += " xposition='" + this.getPosition().x + "'";
  ret += " yposition='" + this.getPosition().y + "'";
  ret += " height='" + this.getSize().y + "'";
  ret += " width='" + this.getSize().x + "'";
  ret += " zindex='" + get(this.id).style.zIndex + "'";
  ret += ">\n" + escape(this.text) + "\n";
  return ret + "</note>"
}
/**
 * Change the text of a note.
 * @param {string} str the text for the note.
 */
Note.prototype.setText = function(str)
{
  // convert characters from 160-255 to html codepoints (this provides
  // Safari compatability
  var chars = [];
  var i;
  for (i = 0; i < str.length; i++)
  {
    var c = str.charCodeAt(i);
    if (c >= 160 && c <= 255)
      chars.push("&#" + c + ";");
    else
      chars.push(str.charAt(i));
  }
  str = chars.join('');

  if (str != this.text)
  {
    this.parent.changed = true;
    this.text = str;
  }
  var elt = get(this.id);
  elt.innerHTML = this.getHTML();

  // make images undraggable
  var imgs = elt.getElementsByTagName('img');
  for (i = 0; i < imgs.length; i++) {
    if (BROWSER_IE_6 == browser || BROWSER_IE_5 == browser) {
      imgs[i].unselectable = 'on';
    } else {
      imgs[i].onmousedown = retFalse;
    }
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
  var elt = get(this.id);
  this.size.x = parseInt(elt.style.width);
  this.size.y = parseInt(elt.style.height);
}
/**
 * Disable a note (can't be moved or edited).
 */
Note.prototype.disable = function()
{
  this.selectable = this.editable = false;
  var elt = get(this.id);
  elt.title = '';
}
/**
 * Enable a note (can be moved and edited).
 */
Note.prototype.enable = function()
{
  this.selectable = this.editable = true;
  var elt = get(this.id);
  elt.title = strings.NOTE_TOOLTIP;
}
/**
 * When a note is selected from the mini note toolbar, we bring it to the
 * front and flash the background.
 */
Note.prototype.select = function()
{
  this.parent.reZOrder(this.id);
  var self = this;
  var elt = get(this.id);
  elt.style.backgroundColor = (new Color(this.bgColor.toString()))
                                   .hsvadj(0, 0, -0.1);
  // make a new layer with the name
  var d = document.createElement('div');
  d.innerHTML = this.id;
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
  var elt = get(this.id);
  elt.style.zIndex = 0;
  this.parent.reZOrder();
}
/**
 * Move the note relative to its current position.
 * @param {Point} delta a point object
 */
Note.prototype.move = function(delta) {
  var newpos = this.getPosition().add(delta);
  var elt = get(this.id);
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
  var av = parseInt(get(a.id).style.zIndex);
  var bv = parseInt(get(b.id).style.zIndex);
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
var Mouse =
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
    for (var n in workspace.notes)
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
  var elt;
  for (n in this.notes)
  {
    elt = get(this.notes[n].id);
    elt.style.border =  noteBorder + 'px #980000 solid';
  }
}
/**
 * Update the position of note(s) when the user moves the mouse.
 * @param {Mouse} md a reference to the parent mouse object
 */
SelectedObjectDrag.prototype.update = function(md)
{
  var offset = md.curPos.sub(md.downPos);
  var elt;
  for (n in this.notes) {
    elt = get(this.notes[n].id);
    var newPos = this.notes[n].pos.add(offset);
    elt.style.left = newPos.x + 'px';
    elt.style.top = newPos.y + 'px';
    if (BROWSER_MOZILLA == browser) {
      // this is a hack to force ffx to resize the content div
      var c = get('content');
      var d = document.createElement('span');
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
  var md = workspace.mouse;
  var offset = md.curPos.sub(md.downPos);
  if (!offset.equals(new Point(0, 0)))
  {
    var f = {
      title : ((this.notes.length == 1) ? strings.HISTORY_MOVE_NOTE
                                        : strings.HISTORY_MOVE_NOTES),
      notes : this.notes,
      off : offset,
      undo : function()
      {
        var elt;
        for (n in this.notes)
        {
          elt = get(this.notes[n].id);
          pos = this.notes[n].pos;
          elt.style.left = pos.x + 'px';
          elt.style.top = pos.y + 'px';
        }
      },
      redo : function()
      {
        var elt;
        for (n in this.notes)
        {
          elt = get(this.notes[n].id);
          pos = this.notes[n].pos.add(this.off);
          elt.style.left = pos.x + 'px';
          elt.style.top = pos.y + 'px';
        }
      }
    };

    workspace.history.add(f);
  }
  // reset the border color to black
  var elt;
  for (var n in this.notes)
  {
    elt = get(this.notes[n].id);
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
  var elt = get(this.note.id);

  // this depends on which edge they grabbed
  var minSize = 10;
  var offset = md.curPos.sub(md.downPos);
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
    var edit = get(this.note.id + 'text');
    var pSize = this.note.getCorrectedSize();
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
  var curSize = this.note.getSize();

  if (!this.size.equals(curSize))
  {
    var f = {
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
        var elt = get(this.id);
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
var History =
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
      var f = this.undoStack.pop();
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
      var f = this.redoStack.pop();
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
    var elt = get('undoImg');
    if (0 == this.undoStack.length)
    {
      elt.title = strings.HISTORY_UNDO_EMPTY;
      elt.className = 'controlsDisabled';
      elt = get('saveImg');
      elt.className = 'controlsDisabled';
    }
    else
    {
      var tooltip = this.undoStack.length == 1 ? strings.HISTORY_UNDO_TOOLTIP
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
      var tooltip = this.redoStack.length == 1 ? strings.HISTORY_UNDO_TOOLTIP
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
var NotePos =
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
    var ret = new Point(this.x, this.y);

    this.x += 20;
    this.y += 20;
    var s = getPageSize();

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
var workspace =
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
   * collision).
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
      var self = this;
      var f = {
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

    var pos;
    if (!('xPos' in note) || !('yPos' in note)) {
      pos = this.notePos.nextPos(note.width, note.height);
      var content = get('content');
      pos.x += parseInt(content.scrollLeft);
      pos.y += parseInt(content.scrollTop);
    }
    if (!('xPos' in note)) note.xPos = pos.x;
    if (!('yPos' in note)) note.yPos = pos.y;

    if (!('bgcolor' in note)) note.bgcolor = bgColors[0].toString();

    if (!('text' in note)) note.text = '';

    // disable editing of a different note
    this.editOff();

    var newDiv = document.createElement('div');
    newDiv.className = 'note';
    newDiv.id = note.id;
    // work around a safari bug
    if (BROWSER_SAFARI == browser) {
      newDiv.style.opacity = '0.99';
      newDiv.style.overflow = 'hidden';
    }
    get('content').appendChild(newDiv);

    //document.body.innerHTML += newDiv;
    var elt = get(note.id);
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
      var topElt = get(this.topId);
      if (topElt) {
        if (parseInt(note.zIndex) > parseInt(topElt.style.zIndex)) {
          this.topId = note.id;
        }
      } else {
        this.topId = note.id;
      }
    }
    this.numNotes++;
    var nNote = new Note(elt, this, note.text);
    this.notes[nNote.id] = nNote;

    newDiv.onmouseover = hitch(nNote, nNote.mouseOver);
    newDiv.onmouseout = hitch(nNote, nNote.mouseOut);
    newDiv.onmousedown = hitch(nNote, nNote.mouseDown);
    newDiv.onmouseup = hitch(nNote, nNote.mouseUp);
    newDiv.onmousemove = hitch(nNote, nNote.mouseMove);
    newDiv.ondblclick = hitch(nNote, nNote.mouseDblClick);
    newDiv.onselectstart = retFalse;
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
      var offX = this.mouseDown.x - x;
      var offY = this.mouseDown.y - y;
      var content = get('content');
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
      var textbox = get(this.edit.id + 'text');
      textbox.parentNode.onselectstart = retFalse;
      if (BROWSER_SAFARI != browser) {
        textbox.parentNode.style.overflow = 'auto';
      }

      // check to see if the text changed.  add to the
      // undo stack if it did.
      if (textbox.value != this.edit.text)
      {
        var f = {
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
      var nArray = [];
      var i = 0;
      for (var nid in this.notes) {
        nArray[i] = this.notes[nid];
        ++i;
      }

      nArray.sort(cmpNotesZ);

      // set zIndex based on order
      var found = 0;
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
    var elt = get('wsname');
    elt.innerHTML = this.name;
  },

  /**
   * This function acts as an "Expose`" like feature for the notes
   * It sets all of the notes to relative positioning and then grabs
   * the relative location, sets its "top" and "left" properties then
   * resets the positioning to absolute. -sph
   *
   * original patch submitted by Sean Hannan
   *
   * This isn't quite ready for release.
   */
  expose : function() {
    var n, style;
    for (n in this.notes) { //loop through the notes
      var note = this.notes[n];
      var elt = get(note.id);  // get the div
      style = elt.style; //get the note's style

      // save old values
      note.lastX = style.left;
      note.lastY = style.top;
      note.lastWidth = style.width;
      note.lastHeight = style.height;

      //Set the styles so that the notes nicely tile
      style.position = 'relative';
      style.margin = '5px';
      style.left = '';
      style.top = '';
      style.cssFloat = 'left';
      style.styleFloat = 'left';
      style.display = '';
      style.width = parseInt(exposeSize / 100.0 * parseInt(style.width)) + 'px';
      style.height = parseInt(exposeSize / 100.0 * parseInt(style.height)) + 'px';
      style.fontSize = exposeSize + '%';

      //get and set the position of the div
      var pos = findPos(elt);
      style.left = pos.x + 'px';
      style.top = pos.y + 'px';
    }
    //loop through again to make it absolute.
    for (n in this.notes) {
      style = get(this.notes[n].id).style;
      style.position = 'absolute';
      style.cssFloat = 'none';
      style.styleFloat = 'none';
      style.margin = '';
    }
  },

  _expose : function() {
    for (var n in this.notes) {  //loop through notes
      var note = this.notes[n];
      var style = get(note.id).style; //get the Div

      // restore values
      style.top = note.lastY;
      style.left = note.lastX;
      style.width = note.lastWidth;
      style.height = note.lastHeight;
      style.fontSize = '100%';
    }
  },

  /**
   * Filter the visible notes (kind of like a search).  Notes that match
   * the regular expression are moved to the front while other notes are
   * disabled and become mostly transparent.
   * @param {string} text the regular expression to filter by
   */
  filter : function(text) {
    var shouldHide;
    if (text.trim().indexOf(strings.COLOR + ":") == 0) {
      var color = text.trim().substring(strings.COLOR.length + 1);
      shouldHide = function(note) {
        return (colorMap[note.bgColor.toString()] == color);
      };
    }
    else {
      var reg = new RegExp(text, 'i');
      shouldHide = function(note) {
        return reg.test(note.text);
      };
    }

    for (n in this.notes) {
      var note = this.notes[n];
      var elt = get(note.id);
      if (shouldHide(note)) {
        elt.className = 'note';
        if (BROWSER_SAFARI == browser) elt.style.opacity = '0.99';
        if (BROWSER_IE_5 == browser) elt.style.visibility = 'visible';
        note.enable();
        get('m' + note.id).className = 'mininote';
      } else {
        elt.className = 'noteHide';
        elt.style.zIndex = 0;
        if (BROWSER_SAFARI == browser) elt.style.opacity = '0.2';
        if (BROWSER_IE_5 == browser) elt.style.visibility = 'hidden';
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
    var mini = get('mini');
    var vNotes = 0;
    var total = 0;

    for (n in this.notes) {
      ++total;
      if (get('m' + n).className.indexOf("noteHide") == -1)
        ++vNotes;
    }

    // Set the width of the mini div.
    // After the first 9 notes, we add space for additional notes
    // 10 is the width of a note.
    var miniNoteWidth = 10;
    if (BROWSER_IE_5 == browser) miniNoteWidth = 8; // width in IE5.5
    var diff = parseInt(Math.max(0, total - 9) / 2) * miniNoteWidth;
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

    var xmlhttp = getxmlreqobj();
    xmlhttp.onreadystatechange = function() {
        if (4 == xmlhttp.readyState && 200 == xmlhttp.status) {
          workspace.loadlistCallback(xmlhttp, offset);
        }
        if (4 == xmlhttp.readyState) {
          try { // break circular ref
            xmlhttp.onreadystatechange = null;
          } catch (e) { }
        }
    };
    this.createNote( {
      'id' : 'load',
      'height' : '130',
      'width' : '270',
      'bgcolor' : '#ffff30'
    } );
    var txt = strings.LOADVERSIONS_INIT;
    this.notes['load'].setText(txt);
    this.reZOrder('load');

    xmlhttp.open('GET', 'getdates.py?name=' + escape(escape(this.name))
                 + '&offset=' + offset, true);
    xmlhttp.send('');
  },

  loadlistCallback : function(xmlhttp, offset) {
    var loadTimes = xmlhttp.responseText.split('|');
    this.createNote( {
      'id': 'load',
      'height': '130',
      'width': '270',
      'bgcolor': '#ffff30'
    } );
    var txt;
    db('resp: ' + xmlhttp.responseText.trim());
    db('resplen: ' + xmlhttp.responseText.trim().length);
    if (xmlhttp.responseText.trim() == '') {
      txt = this.notes['load'].text + '\n' + strings.LOADVERSIONS_NONE;
    } else {
      txt = strings.LOADVERSIONS_ABOUT
            + '<div style="text-align: center;"><form method="GET" action="load.py" onmousedown="event.cancelBubble=true;">'
            + '<input type="hidden" name="name" value="'
            + escape(this.name) + '" /><select class="loadfrm" name="time">';
      for (var i = 0; i < loadTimes.length && i < numDates; i++) {
        txt += "<option value='" + loadTimes[i].trim() + "'>"
               + (new MyDate(loadTimes[i])) + "</option>";
      }

      txt += "</select><input type='submit' value='load' /><br /><span style='font-size: 0.8em;'>";

      if (loadTimes.length > numDates)
      {
        txt += "&laquo; <a class='fakelink' onclick='workspace.loadlist("
             + (offset+numDates) + ")'>" + strings.LOADVERSIONS_OLDER + "</a> ";
        if (offset > 0)
          txt += "| ";
      }
      if (offset > 0) {
        txt += "<a class='fakelink' onclick='workspace.loadlist("
             + (offset-numDates) + ")'>" + strings.LOADVERSIONS_NEWER + "</a> &raquo;";
      }

      txt += "<br />" + strings.LOADVERSIONS_DATE_NOTE + "</span></div>";
    }
    this.notes['load'].setText(txt);
    this.reZOrder('load');
  },

  /**
   * Checks to see if the notes have changed since the last time we loaded
   * the notes.
   */
  checkUpdated : function() {
    var xmlhttp = getxmlreqobj();
    xmlhttp.onreadystatechange = function() {
        if (4 == xmlhttp.readyState && 200 == xmlhttp.status) {
          db('check updated');
          workspace.checkUpdatedCallback(xmlhttp);
        }
        if (4 == xmlhttp.readyState) {
          try { // break circular ref
            xmlhttp.onreadystatechange = null;
          } catch (e) { }
        }
    };
    xmlhttp.open('GET', 'getrecent.py?name=' + escape(escape(workspace.name)), true);
    xmlhttp.send('');
  },

  checkUpdatedCallback : function(xmlhttp) {
    var loadTime = xmlhttp.responseText.trim();
    if (loadTime > this.loadedTime) {
      this.createNote( {
        'id' : 'updated',
        'xPos' : '0',
        'yPos' : '40',
        'bgcolor' : '#ff6060',
        'height' : '100',
        'text' : strings.COLLISION_WARNING.replace("$1", escape(escape(escape(this.name))))
      });
    } else {
      db('no changes');
      window.setTimeout(workspace.checkUpdated, workspace.updateInterval);
    }
  },

  /**
   * Generate the xml representation of the workspace (used when saving).
   * @type string
   */
  toString : function()
  {
    var ret = "<workspace name='" + escape(this.name) + "'";
    ret += " nextNoteNum='" + this.nextNoteNum + "'";
    ret += ">\n";
    for (nid in this.notes)
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
    var max = -1;
    var maxNote;
    var noteArr = [];
    // determine which note is on top
    for (var n in this.notes)
    {
      var cur = parseInt(get(this.notes[n].id).style.zIndex);
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
    var s = get('saveImg');
    if (s.className == 'controlsDisabled')
      return;

    s.src = 'images/saving.gif';
    this.editOff();

    var xmlhttp = getxmlreqobj();
    xmlhttp.onreadystatechange = function() {
        if (4 == xmlhttp.readyState && 200 == xmlhttp.status) {
          workspace.saveCallback(xmlhttp);
        } else if (4 == xmlhttp.readyState) {
          db(xmlhttp.status);
          alert(strings.SAVE_STATUS_ERROR);
          get('saveImg').src = 'images/save.gif';
        }
        if (4 == xmlhttp.readyState) {
          try { // break circular ref
            xmlhttp.onreadystatechange = null;
          } catch (e) { }
        }
    };

    xmlhttp.open('POST', 'save.py', true);
    var savePayload = workspace.toString();
    xmlhttp.send(savePayload);
  },
  saveCallback : function(xmlhttp) {
    var doc = xmlhttp.responseXML;
    db("response: " + xmlhttp.responseText);
    if (doc && 'ok' == doc.getElementsByTagName('status')[0].getAttribute('value')) {
      this.changed = false;
      get('saveImg').className = 'controlsDisabled';
      this.loadedTime = doc.getElementsByTagName('status')[0].getAttribute('update');
    } else {
      alert(strings.SAVE_SERVER_ERROR.replace("$1", adminEmail));
    }
    get('saveImg').src = 'images/save.gif';
  }
};

///
/// global methods
///

/**
 * Write a message to the debug textarea.
 * @param {string} msg the message to display
 */
function db(msg) {
  if (debugOn) {
    var elt = get('debug');
    elt.value = msg + '\n' + elt.value;
  }
}

/**
 * Initialize the workspace.
 */
function init()
{
  if (debugOn) {
    get('db').innerHTML = "<form name='debugForm'>" +
        "<textarea style='position:absolute;top:26px;right:10px;' id='debug' cols='50' rows='10'></textarea>" +
        "<input type='button' onclick='document.debugForm.debug.value=\"\";' value='clear' />" +
        "</form>";
    get('debug').value = '';
  }

  // preload the close image
  var closeImg = new Image();
  closeImg.src = 'images/close.gif';

  // a hack for safari compatability
  if (BROWSER_SAFARI == browser) {
    escape = myescape;
    //unescape = myunescape;
  }

  var toolbar = get('toolbar');
  toolbar.onmousedown = cancelBubble;
  if (BROWSER_SAFARI == browser) {
    toolbar.style.width = '100%';
  }

  workspace.mouse.curPos = new Point();
  document.onmousemove = docMouseMove;
  document.onkeydown = docKeyDown;
  get('content').onmousedown = docMouseDown;
  document.onmouseup = docMouseUp;

  /**
   * When the user navigates away, if there are changes, give the user a
   * chance to cancel.
   */
  window.onbeforeunload = winBeforeUnload;

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
  window.setTimeout(workspace.checkUpdated, workspace.updateInterval);
}

function cancelBubble(e) {
  e = e || window.event;
  e.cancelBubble = true;
}
// absolute mouse positions; modified from:
// http://www.web-wise-wizard.com/javascript-tutorials/javascript-mouse-event-handlers.html
function docMouseMove(e) {
  e = e || window.event;
  var x, y;
  if (!e.pageX && !e.pageY) { // IE
    x = e.x+document.body.scrollLeft;
    y = e.y+document.body.scrollTop;
  } else {
    x = e.pageX;
    y = e.pageY;
  }
  workspace.mouse.update(x, y);
  workspace.docMove(x, y);
}

/**
 * If the user clicks on the background, turn off note editing and start dragging
 */
function docMouseDown(e) {
  workspace.editOff();

  // force a position update
  docMouseMove(e);
  var curPos = workspace.mouse.curPos.copy();
  // get rel pos
  // make sure we're not on a scrollbar
  var content = get('content');
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
  if (!ev) ev = window.event;

  var key = String.fromCharCode(ev.keyCode);
  if (workspace.shortcuts && !workspace.edit) {
    // blah, I should turn this into a map
    var n;
    if ('P' == key && ev.altKey) {
      var note = workspace.createNote();
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
        + escape(escape(escape(workspace.name))) + "&via='+e(location.href)+'&nn='+e(txt);\">"
        + strings.BOOKMARKLET_NAME + "</a>"});
    } else { // forward to note
      n = workspace.notes[workspace.topId];
      n.keyDown(ev);
    }
    /* //not ready yet
    else if ('E' == key) {
      workspace.expose();
      // swap the functionality with the undo effect
      var tmp = workspace.expose;
      workspace.expose = workspace._expose;
      workspace._expose = tmp;
    }
    */
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

/**
 * Give the user a chance to save workspace.
 */
function winBeforeUnload() {
  if (workspace.changed && workspace.name != "Sample Workspace") {
    return strings.UNLOAD_WARNING;
  }
}

/**
 * Get the size of the browser.
 * @type Point
 */
function getPageSize() {
  var ret;
  if (BROWSER_IE_5 == browser) {
    ret = new Point(document.body.clientWidth, document.body.clientHeight);
  } else if (BROWSER_IE_6 == browser) {
    ret = new Point(document.documentElement.clientWidth,
            document.documentElement.clientHeight);
  } else {
    ret = new Point(window.innerWidth, window.innerHeight);
  }
  return ret;
}

/**
 * Get a reference to an html object given the id.
 * @param {string} id id of the object
 * @type {HtmlElement}
 */
function get(id) { return document.getElementById(id); }
