/**
 * @fileoverview A few generic objects and functions used by webnote.
 */

///
/// global methods
///
var hex2dec = function(h) { return parseInt(h, 16); };
var dec2hex = function(d)
{
  var ret = d.toString(16);
  while (ret.length < 2)
    ret = '0' + ret;
  return ret;
};

var getxmlreqobj = function() {
  var xmlobj;
  try { xmlobj = new ActiveXObject("Msxml2.XMLHTTP"); }
  catch (e) {
    try { xmlobj = new XMLHttpRequest(); }
    catch (e) { alert("Your browser doesn't support xmlhttprequest.  Unable to save."); }
  }
  return xmlobj;
};

String.prototype.trim = function() {
  return this.replace(/^\s+/, '').replace(/\s+$/, '');
}

/**
 * Reduces the chance of a closure leaking memory by minimizing the scope.
 */
var hitch = function(obj, meth) {
  return function() { return meth.apply(obj, arguments); };
}

var retFalse = function() { return false; };

/**
 * modified from http://www.quirksmode.org/js/findpos.html
 * to return a Point()
 */
function findPos(obj)
{
  var cur = new Point();
  if (obj.offsetParent)
  {
    while (obj.offsetParent)
    {
      cur.x += obj.offsetLeft;
      cur.y += obj.offsetTop;
      obj = obj.offsetParent;
    }
  }
  else if (obj.x)
  {
    cur.x += obj.x;
    cur.y += obj.y;
  }
  return cur;
}

/**
 * Get the position of a mouse event relative to the event target.
 */
function findRelativeMousePos(ev) {
  var target = ev.target || ev.srcElement;
  var targetPos = findPos(target);
  var pagePos;
  if (ev.pageX || ev.pageY) {
    pagePos = new Point(ev.pageX, ev.pageY);
  } else if (ev.clientX || ev.clientY) {
    pagePos = new Point(ev.clientX + document.body.scrollLeft,
                        ev.clientY + document.body.scrollTop);
  }
  // Take content scroll into account since we use that instead of
  // scrolling the body.
  var content = get('content');
  pagePos.x += content.scrollLeft;
  pagePos.y += content.scrollTop;

  return pagePos.sub(targetPos);
}

///
/// generic objects
///

/**
 * Create a new Point object
 * @class A class representing two numbers, x and y.
 * @param {int} x optional parameter for x
 * @param {int} y optional parameter for y
 * @constructor
 */
function Point(x, y)
{
  /**
   * @type int
   */
  this.x = x || 0;
  /**
   * @type int
   */
  this.y = y || 0;
}
/**
 * Add two points together and return a new Point object.
 * @param {Point} rhs The point object to add.
 * @return a new point object
 * @type Point
 */
Point.prototype.add = function(rhs) { return new Point(this.x + rhs.x, this.y + rhs.y); };
/**
 * Subtract the input point from the object and return a new Point object.
 * @param {Point} rhs The point to subtract.
 * @return a new point object
 * @type Point
 */
Point.prototype.sub = function(rhs) { return new Point(this.x - rhs.x, this.y - rhs.y); };
/**
 * Divide x and y by the input value.
 * @param {int} n a number to divide by
 * @return a new point object
 * @type Point
 */
Point.prototype.div = function(n) { return new Point(this.x/n, this.y/n); };
/**
 * Make a copy of the Point object.
 * @return a new point object
 * @type Point
 */
Point.prototype.copy = function() { return new Point(this.x, this.y); };
/**
 * Determines if two points have the samve x and y values, respectively.
 * @param {Point} rhs the point to compare
 * @type boolean
 */
Point.prototype.equals = function(rhs) { return this.x == rhs.x && this.y == rhs.y; };
/**
 * A string representation of a point.
 * @return E.g., "(1, 2)"
 * @type String
 */
Point.prototype.toString = function() { return '(' + this.x + ', ' + this.y + ')'; };


/**
 * Create a new object to represent a color in HSV.
 * @class A class representing a color as HSV values.
 * @param {Color} rgb A Color object (RGB) to convert to HSV.
 * @constructor
 */
function ColorHSV(rgb)
{
  var r = rgb.r / 255.0; var g = rgb.g / 255.0; var b = rgb.b / 255.0;
  
  var min = Math.min(r, g, b); var max = Math.max(r, g, b);
  this.v = max;
  var delta = max - min;
  if (0 == max) // r == g == b == 0
  {
    this.s = 0;
    this.h = -1;
  }
  else
  {
    this.s = delta / max;
    if (r == max)
      this.h = (g - b) / delta;
    else if (g == max)
      this.h = 2 + (b - r) / delta;
    else
      this.h = 4 + (r - g) / delta;
    this.h *= 60.0;
    if (!this.h) // shades of grey have no value
      this.h = 0;
    if (this.h < 0)
      this.h += 360.0;
  }
}
/**
 * A pretty way to write out the value of a HSV point.
 * @type String
 */
ColorHSV.prototype.toString = function()
{
  return '(' + this.h + ', ' + this.s + ', ' + this.v + ')';
};
/**
 * Convert the HSV value to RGB and return a Color object.
 * @type Color
 */
ColorHSV.prototype.toColor = function()
{
  var ret = new Color('000000');
  if (0 == this.s)
  {
    ret.r = ret.g = ret.b = parseInt(this.v*255.0);
    return ret;
  }
  var h = this.h / 60.0;
  var i = Math.floor(h);
  var f = h - i;
  var p = this.v * (1.0 - this.s);
  var q = this.v * (1.0 - this.s * f);
  var t = this.v * (1.0 - this.s * (1 - f));
  switch (i)
  {
    case 0: ret.r = this.v; ret.g = t; ret.b = p; break;
    case 1: ret.r = q; ret.g = this.v; ret.b = p; break;
    case 2: ret.r = p; ret.g = this.v; ret.b = t; break;
    case 3: ret.r = p; ret.g = q; ret.b = this.v; break;
    case 4: ret.r = t; ret.g = p; ret.b = this.v; break;
    case 5: ret.r = this.v; ret.g = p; ret.b = q; break;
    default:
      db('error coverting from hsv to rgb');
  }
  ret.r = parseInt(ret.r*255.0);
  ret.g = parseInt(ret.g*255.0);
  ret.b = parseInt(ret.b*255.0);
  return ret;
};
/**
 * Adjust the HSV values of the object.  Returns a reference to the same
 * object.
 * @param {int} h hue adjustment
 * @param {int} s saturation adjustment
 * @param {int} v luminance adjustment
 * @type ColorHSV
 */
ColorHSV.prototype.adj = function(h, s, v)
{
  this.h += h; this.s += s; this.v += v;
  
  if (h < 0)
    h += 360.0;
  if (h > 360)
    h -= 360.0;
  this.s = Math.min(1.0, this.s); this.s = Math.max(0.0, this.s);
  this.v = Math.min(1.0, this.v); this.v = Math.max(0.0, this.v);
  return this;
};


/**
 * Create a new object to represent a color as RGB values.
 * @class A class representing a color as RGB values.
 * @param {String} value A string representing the color.  It can be in any
 * of the following formats: rgb(##, ##, ##), #ffffff, or ffffff
 * @constructor
 */
function Color(value)
{
  // constuctor creates object from a string
  // I keep all values in decimal
  if (value.charAt(0) == 'r')
  {
    /**
     * @type int
     */
    this.r = parseInt(value.substring(4));
    var pos = value.indexOf(',');
    /**
     * @type int
     */
    this.g = parseInt(value.substring(pos+1));
    pos = value.indexOf(',', pos+1);
    /**
     * @type int
     */
    this.b = parseInt(value.substring(pos+1));
  }
  else
  {
    if (value.charAt(0) == '#')
      value = value.substring(1, 7);
    this.r = hex2dec(value.substring(0, 2));
    this.g = hex2dec(value.substring(2, 4));
    this.b = hex2dec(value.substring(4, 6));
  }
}
/**
 * Convert the object to a string of the form #ffffff
 * @type String
 */
Color.prototype.toString = function()
{
  return '#' + dec2hex(this.r) + dec2hex(this.g) + dec2hex(this.b);
};
/**
 * Adjust the HSV values of the color.  Returns a reference to the
 * object.
 * @param {float} h hue
 * @param {float} s saturation
 * @param {float} v luminance
 * @type Color
 */
Color.prototype.hsvadj = function(h, s, v)
{
  var hsv = new ColorHSV(this);
  hsv.adj(h, s, v);
  var c = hsv.toColor();
  this.r = c.r; this.g = c.g; this.b = c.b;
  return this;
};

var weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
var months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
              'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
/**
 * Create a new MyDate object.
 * @class A class representing the current date and time.
 * @param {String} a string of the form "YY-MM-DD HH:mm:ss"
 * @constructor
 */
function MyDate(stddt)
{
  var tokens = stddt.split(' ');
  var dateTokens = tokens[0].split('-');
  var timeTokens = tokens[1].split(':');

  var dateobj = new Date();
  dateobj.setYear(dateTokens[0]);
  dateobj.setMonth(dateTokens[1] - 1);
  dateobj.setDate(dateTokens[2]);
  dateobj.setHours(timeTokens[0]);
  dateobj.setMinutes(timeTokens[1]);
  dateobj.setSeconds(timeTokens[2]);

  /**
   * @type Date
   */
  this.dateobj = dateobj;
  return this;
}
/**
 * Left pad a number with zeros (up to 2 digits).  For example, 4 to "04".
 * @type String
 */
MyDate.prototype.pad = function(s)
{
  while (s.toString().length < 2)
    s = '0' + s;
  return s;
};
/**
 * A pretty (well, to me at least) string representing the date.
 * For example, "Mon Jan 4, 13:42:12"
 * @type String
 */
MyDate.prototype.toString = function()
{
  var d = this.dateobj;
  return weekdays[d.getDay()] + ' ' + months[d.getMonth()] + ' ' + d.getDate()
          + ', ' + this.pad(d.getHours()) + ':' + this.pad(d.getMinutes())
          + ':' + this.pad(d.getSeconds());
};

function isEsc(s)
{
  return !( (s >= 48 && s <= 57) || (s >= 65 && s <= 90) 
            || (s >=97 && s <=122) || s == 95);
}
/**
 * myescape and myunescape are provided for safari compatability.
 * In reality, I should be using encodeURIComponent for everything, but
 * since information in the db is already in this form, we'll keep it
 * in this form.
 */
function myescape(s)
{
  var ret = "";
  for (var i = 0; i < s.length; i++)
  {
    if (isEsc(s.charCodeAt(i)))
    {
      var hex = dec2hex(s.charCodeAt(i)).toUpperCase();
      if (hex.length > 2)
        hex = 'u' + hex;
      ret += '%' + hex;
    }
    else
      ret += s[i];
  }
  return ret;
}

/**
 * myescape and myunescape are provided for safari compatability.
 * In reality, I should be using encodeURIComponent for everything, but
 * since information in the db is already in this form, we'll keep it
 * in this form.
 */
function myunescape(s)
{
  var ret = "";
  for (var i = 0; i < s.length; i++)
  {
    if ('%' == s[i])
    {
      if (s.length > i+5 && s[i+1] == 'u')
      {
        ret += String.fromCharCode(hex2dec(s.substr(i+2, i+6)));
        i += 5;
      }
      else if (s.length > i+2)
      {
        ret += String.fromCharCode(hex2dec(s.substr(i+1, i+3)));
        i += 2;
      }
    }
    else
      ret += s[i];
  }
  return ret;
}
