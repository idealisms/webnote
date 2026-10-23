"""HTTP golden cases for the Go port: requests run through the Python app.

Called by gen_golden.py. Each case runs against a datastore holding SEED, and
records the response's status, some headers and the body. Saves run against a
fresh copy of SEED with a fixed clock, and also record what they stored.
"""

import base64
import datetime
import datetime as _datetime_module
import json
import os
import zlib
from unittest import mock

import webtest
from google.appengine.api import datastore
from google.appengine.api import datastore_types
from google.appengine.ext import ndb
from google.appengine.ext import testbed

import main
import models
import save_handler

# The time saves see (Python's save truncates the microseconds).
SAVE_NOW = datetime.datetime(2026, 10, 22, 15, 4, 5, 123456,
                             tzinfo=datetime.timezone.utc)

HEADERS = ('Content-Type', 'Location', 'Allow')


def ndb_json(value):
    return models.Notes.notesJsonArray._to_base_type(value)


def note(id, text, **attrs):
    ret = {'id': id}
    ret.update(attrs)
    ret['text'] = text
    return ret


def utc(*args):
    return datetime.datetime(*args)


def seed():
    """Workspaces and revisions to load before each case.

    Returns (workspaces, notes). Blobs are given raw, so seeds can hold any
    stored format.
    """
    workspaces = []
    notes = []

    def ws(name, time, next_note_num=1, key=None):
        workspaces.append({
            'key': key or models.Workspace.key_id(name),
            'name': name,
            'nextNoteNum': next_note_num,
            'time': time,
        })
        return workspaces[-1]['key']

    def rev(key, time, blob, meaning22=False):
        if not isinstance(blob, bytes):
            blob = zlib.compress(ndb_json(blob))
        notes.append({'workspace': key, 'time': time, 'blob': blob,
                      'meaning22': meaning22})

    sample = ws('Sample%20Workspace', utc(2026, 7, 4, 16, 30, 15), 5)
    rev(sample, utc(2025, 12, 31, 5, 0, 0), [note('note0', 'old')])
    rev(sample, utc(2026, 3, 8, 6, 30, 0), [
        note('note0', 'Welcome%20to%20Webnote', bgcolor='#ffff99',
             xPos='10', yPos='20', height='100', width='200', zIndex='1'),
    ], meaning22=True)
    rev(sample, utc(2026, 7, 4, 16, 30, 15), [
        note('note0', 'Welcome%20to%20Webnote%21%0ASecond%20line',
             bgcolor='#ffff99', xPos='10', yPos='20', height='100',
             width='200', zIndex='1'),
        note('note3', '%u2665%20caf%E9%20%3Cb%3Ebold%3C/b%3E', bgcolor='#ccf'),
        note('note4', '<b>raw & html</b> "quoted" \'single\' </script>'),
        note('note5', '%20%20padded%20%20'),
    ], meaning22=True)

    dashed = ws('my%2Dnotes', utc(2017, 12, 31, 0, 0, 0), 2)
    rev(dashed, utc(2017, 12, 31, 0, 0, 0), [note('note0', 'hi')])

    ws('empty%20ws', utc(2017, 12, 31, 5, 0, 0), 0)

    many = ws('many', utc(2017, 12, 31, 17, 0, 0), 3)
    for h in range(12):
        rev(many, utc(2017, 12, 31, 5 + h, 0, 0), [note('note0', str(h))])
    # Two revisions in the same second.
    rev(many, utc(2017, 12, 31, 17, 0, 0), [note('note0', 'a')])
    rev(many, utc(2017, 12, 31, 17, 0, 0), [note('note0', 'b')])

    # Revisions around the fall-back hour, by load time.
    dst = ws('dst', utc(2017, 11, 5, 7, 0, 0))
    for t in (utc(2017, 11, 5, 5, 30, 0), utc(2017, 11, 5, 6, 30, 0),
              utc(2017, 11, 5, 7, 0, 0)):
        rev(dst, t, [note('note0', t.isoformat())])

    ws('caf%E9', utc(2017, 12, 31))
    ws('%u5D9', utc(2017, 12, 31))
    ws('Bogot%E1', utc(2017, 12, 31))
    ws('a%27b%3Cc', utc(2017, 12, 31))

    def rss_ws(name, *texts):
        key = ws(name, utc(2026, 1, 2, 3, 4, 5))
        rev(key, utc(2026, 1, 2, 3, 4, 5),
            [t if isinstance(t, dict) else note('n%d' % i, t)
             for i, t in enumerate(texts)])

    rss_ws('rss',
           'x' * 70 + '%0Asecond%20line',
           '%20%0A%20first%0Asecond',
           'bad%ZZ%2%u00E9%41%',
           '&amp; <tag> "q" \'s\' > ]]>',
           {'id': 'n8'},
           {'text': 'no id'},
           '',
           note('id with spaces & <amp> "q"', 'odd id'),
           'line\r\nwith crlf%0D%0Aand%0D',
           '%F0%9F%98%80%20%u2665%20\u2665\U0001f600',
           '\x01\x1f ctl \x7f')
    # Characters U+0080-U+00FF are written as Latin-1 bytes, which webapp2
    # decodes as UTF-8: a 500 unless the bytes happen to be valid UTF-8.
    rss_ws('rss%20latin1', 'caf\u00e9')
    rss_ws('rss%20latin1%20escaped', 'caf%E9')
    rss_ws('rss%20utf8%20escaped', 'caf%C3%A9')
    rss_ws('rss%20valid%20utf8', '\u00c3\u00a9')
    rss_ws('rss%20split', '\u00c3', '\u00a9')
    rss_ws('rss%20nbsp', '\u00a0')
    rss_ws('rss%20bad%20utf8', '%E1%80%41%C3%28%F0%9F%98%ED%A0%80%C0%80%FF')
    rss_ws('rss%20long', '\u2665' * 70)
    rss_ws('rss%20strip', '\u3000\x1c padded \x85')
    ws('caf%C3%A9%20rss', utc(2017, 12, 31))

    def blob_ws(name, blob, meaning22=False):
        key = ws(name, utc(2017, 12, 31))
        rev(key, utc(2017, 12, 31), blob, meaning22)

    def z(text):
        return zlib.compress(text.encode('utf8'))

    blob_ws('blob%20dict', z('{"id":"note0","text":"x"}'))
    blob_ws('blob%20null', z('null'))
    blob_ws('blob%20string', z('"text"'))
    blob_ws('blob%20numbers', z('[1, 1.5e3, -0, 1e100, 2.5e-7, 12345678901234567890, 0.1, 1E5, -0.0]'))
    blob_ws('blob%20nested', z('[{"id":"n","text":"t","extra":[true,false,null,{"a":1}],"b":{}}]'))
    blob_ws('blob%20dupkeys', z('[{"id":"a","text":"t","id":"b"}]'))
    blob_ws('blob%20nondict', z('["a", 1]'))
    blob_ws('blob%20nonstring%20text', z('[{"id":"a","text":5}]'))
    blob_ws('blob%20nonstring%20id', z('[{"id":5,"text":"t"},{"id":null,"text":"t"},{"id":true,"text":"t"},{"id":1.5,"text":"t"}]'))
    blob_ws('blob%20unicode', z('[{"id":"a","text":"\\u00e9 \\u2028 \\ud83d\\ude00 / <\\/script>"}]'))
    blob_ws('blob%20not%20json', z('not notes'))
    blob_ws('blob%20raw%20json', b'[{"id": "raw", "text": "raw"}]')
    blob_ws('blob%20invalid%20utf8', b'\x80 not notes')
    blob_ws('blob%20raw%20not%20json', b'not notes')
    blob_ws('blob%20overflow', z('[1e400, -1e400, 1e-400]'))

    return workspaces, notes


def put_seed(workspaces, notes):
    for w in workspaces:
        entity = datastore.Entity('Workspace', name=w['key'])
        entity['name'] = w['name']
        entity['nextNoteNum'] = w['nextNoteNum']
        entity['time'] = w['time']
        datastore.Put(entity)
    for n in notes:
        ws_key = ndb.Key('Workspace', n['workspace'])
        if n['meaning22']:
            from gen_golden import put_compressed_notes
            put_compressed_notes(ws_key, n['time'], n['blob'])
        else:
            entity = datastore.Entity('Notes')
            entity['workspaceKey'] = ws_key.to_old_key()
            entity['time'] = n['time']
            entity['notesJsonArray'] = datastore_types.Blob(n['blob'])
            entity.set_unindexed_properties(['notesJsonArray'])
            datastore.Put(entity)


def get(path, **headers):
    return {'method': 'GET', 'path': path, 'headers': headers}


def requests():
    """Requests that don't write."""
    reqs = [
        get('/'), get('/webnote'), get('/webnote/index.html'),
        get('/?x=1'), get('/webnote/index.html?x=1'),
        get('/foo'), get('/webnote/'), get('/webnotes'),
        {'method': 'POST', 'path': '/', 'headers': {}},
    ]
    for lang in (None, 'de-DE,de;q=0.9', 'en-US', 'fr', 'it,de,en-US,en;q=0.9',
                 'EN-gb', ' de ', ';', ''):
        reqs.append(get('/webnote/strings.js',
                        **({} if lang is None else {'Accept-Language': lang})))

    # The workspace page.
    for path in [
        '/webnote/Sample%2520Workspace',
        '/webnote/Sample%2520Workspace?time=2026-03-08%2001:30:00',
        '/webnote/Sample%2520Workspace?time=2026-3-8+1:30:0',
        '/webnote/Sample%2520Workspace?time=2025-12-31%2000:00:00',
        '/webnote/Sample%2520Workspace?time=2025-12-31%2000:00:01',
        '/webnote/Sample%2520Workspace?time=bad',
        '/webnote/Sample%2520Workspace?time=',
        '/webnote/Sample%2520Workspace?time=9999-12-31%2000:00:00',
        '/webnote/Sample%2520Workspace?time=2026-03-08%2001:30:00&time=bad',
        '/webnote/Sample%20Workspace',
        '/webnote/Sample Workspace',
        '/webnote/Sample+Workspace',
        '/webnote/load.py?name=Sample%2520Workspace',
        '/webnote/load.py?name=Sample%20Workspace',
        '/webnote/load.py?name=Sample+Workspace',
        '/webnote/load.py?name=Sample%2520Workspace&time=2025-12-31%2000:00:00',
        '/webnote/load.py?name=Sample%2520Workspace&name=other',
        '/webnote/load.py?name=',
        '/webnote/load.py',
        '/webnote/load.py?nam=x',
        '/webnote/my%25252Dnotes',
        '/webnote/my-notes',
        '/webnote/my%252Dnotes',
        '/webnote/empty%252520ws',
        '/webnote/new',
        '/webnote/many',
        '/webnote/dst?time=2017-11-05%2001:30:00',
        '/webnote/dst?time=2017-11-05%2001:00:00',
        '/webnote/dst?time=2017-11-05%2002:00:00',
        '/webnote/caf%2525E9',
        '/webnote/caf%25E9',
        '/webnote/caf%C3%A9',
        '/webnote/caf%E9',
        '/webnote/Bogot%C3%A1',
        '/webnote/Bogot%E1',
        '/webnote/%252525u5D9',
        '/webnote/%2525u5D9',
        '/webnote/%25u05D9',
        '/webnote/a%2527b%25253Cc',
        "/webnote/a'b%3Cc",
        '/webnote/a%0Ab',
        '/webnote/a%0Db',
        '/webnote/a%09b',
        '/webnote/a%00b',
        '/webnote/a/b',
        '/webnote/a%2Fb',
        '/webnote/%',
        '/webnote/%25',
        '/webnote/%zz',
        '/webnote/.xml',
        '/webnote/x.xml.xml',
        '/webnote/x.XML',
        '/webnote/rss.py',
        '/webnote/save.py',
        '/webnote/getrecent.py/x',
        '/webnote/strings.jsx',
        '/webnote/%F0%9F%98%80',
        '/webnote/%ED%A0%80',
        '/webnote/%EF%BB%BFbom',
        # Python's $ also matches before a final newline.
        '/webnote/my-notes%0A',
        '/webnote/my-notes%0A%0A',
        '/webnote/my-notes.xml%0A',
        '/webnote/strings.js%0A',
        '/webnote/getrecent.py%0A?name=many',
        '/%0A',
        '/webnote/%0A',
    ]:
        reqs.append(get(path))
    reqs.append(get('/webnote/Sample%2520Workspace', Host='example.com:8080'))
    # Query string parsing, seen through the name on the page.
    for query in ('name=%zz', 'name=a;b', 'name=a%2', 'name=a%E9', 'x=%E9&name=a',
                  'name=a&x=%E9', 'name', 'name=', '=x&name=b', '&name=c&',
                  'name=a+b%2Bc', 'name=%C3%A9%u00E9', 'name=a=b', 'NAME=a',
                  'name=%00', 'name=%0A', 'name=%EF%BB%BFx', 'name=x&name=y',
                  'name=%ED%A0%80', 'name=%F4%90%80%80', 'name=%C0%80'):
        reqs.append(get('/webnote/load.py?' + query))
    reqs.append(get('/webnote/missing?x=%E9'))
    reqs.append(get('/webnote/Sample%2520Workspace?x=%E9'))
    for name in ('blob%2520dict', 'blob%2520null', 'blob%2520string',
                 'blob%2520numbers', 'blob%2520nested', 'blob%2520dupkeys',
                 'blob%2520nondict', 'blob%2520nonstring%2520text',
                 'blob%2520nonstring%2520id', 'blob%2520unicode',
                 'blob%2520not%2520json', 'blob%2520raw%2520json',
                 'blob%2520invalid%2520utf8', 'blob%2520raw%2520not%2520json',
                 'blob%2520overflow',
                 'rss', 'rss%2520latin1', 'rss%2520latin1%2520escaped',
                 'rss%2520utf8%2520escaped', 'rss%2520valid%2520utf8',
                 'rss%2520split', 'rss%2520nbsp', 'rss%2520bad%2520utf8',
                 'rss%2520long', 'rss%2520strip', 'caf%2525C3%2525A9%2520rss'):
        reqs.append(get('/webnote/' + name))
        reqs.append(get('/webnote/' + name + '.xml'))

    # RSS.
    for path in [
        '/webnote/Sample%2520Workspace.xml',
        '/webnote/Sample%2520Workspace.xml?time=2025-12-31%2000:00:00',
        '/webnote/rss.py?name=Sample%2520Workspace',
        '/webnote/rss.py?name=',
        '/webnote/rss.py',
        '/webnote/missing.xml',
        '/webnote/empty%252520ws.xml',
        '/webnote/my-notes.xml',
        '/webnote/caf%2525E9.xml',
        '/webnote/%252525u5D9.xml',
        '/webnote/a%2527b%25253Cc.xml',
    ]:
        reqs.append(get(path))
    reqs.append(get('/webnote/rss.xml', Host='example.com:8080'))

    # getrecent.py and getdates.py.
    for name in ('Sample%2520Workspace', 'Sample%20Workspace', 'my-notes',
                 'my%252Dnotes', 'many', 'missing', '', 'caf%25E9', 'caf%E9',
                 'caf%C3%A9', 'Bogot%E1', '%2525u5D9', 'a%2527b%25253Cc'):
        reqs.append(get('/webnote/getrecent.py?name=' + name))
        reqs.append(get('/webnote/getdates.py?name=' + name))
    reqs.append(get('/webnote/getrecent.py'))
    reqs.append(get('/webnote/getdates.py'))
    reqs.append(get('/webnote/getrecent.py?name=many&name=missing'))
    for offset in ('0', '10', '11', '12', '13', '100', '-1', '-14', 'abc', ' 3 ',
                   '+2', '1_0', '1__0', '_1', '99999999999999999999',
                   '2147483648', '0x10', '1.5', '', '%EF%BC%91', '%E1'):
        reqs.append(get('/webnote/getdates.py?name=many&offset=' + offset))
    reqs.append(get('/webnote/getdates.py?offset=1&name=many'))

    # Methods.
    for method in ('POST', 'PUT', 'DELETE', 'HEAD', 'OPTIONS', 'PATCH'):
        for path in ('/webnote/Sample%2520Workspace', '/webnote/getrecent.py',
                     '/webnote/getdates.py', '/webnote/strings.js',
                     '/webnote/rss.xml', '/webnote/load.py?name=x'):
            reqs.append({'method': method, 'path': path, 'headers': {}})
    for path in ('/webnote/save.py', '/'):
        for method in ('GET', 'HEAD', 'PUT'):
            reqs.append({'method': method, 'path': path, 'headers': {}})
    # POSTed form parameters count too (webob's request.get).
    reqs.append({'method': 'POST', 'path': '/webnote/save.py?name=x',
                 'headers': {}, 'body': ''})
    return reqs


def client_body(name, notes, next_note_num='1'):
    """A save body the way webnote.js builds it."""
    ret = "<workspace name='%s' nextNoteNum='%s'>\n" % (name, next_note_num)
    for attrs, text in notes:
        ret += '<note %s>\n%s\n</note>\n' % (
            ' '.join("%s='%s'" % kv for kv in attrs), text)
    return ret + '</workspace>'


ATTRS = [('noteid', 'note0'), ('bgcolor', '#ffff99'), ('xposition', '10'),
         ('yposition', '20'), ('height', '100'), ('width', '200'),
         ('zindex', '1')]


def save_bodies():
    bodies = [
        client_body('new%20ws', [(ATTRS, 'hello%20world')], '3'),
        client_body('Sample%20Workspace', [(ATTRS, 'edited')], '6'),
        client_body('my-notes', [(ATTRS, 'from%20firefox')]),
        client_body('Bogotá', [(ATTRS, 'x')]),
        client_body('new', []),
        client_body('', [(ATTRS, 'empty name')]),
        client_body('new', [(ATTRS, 'a'), ([('noteid', 'note1')], 'b')]),
        client_body('new', [([('zindex', '2'), ('noteid', 'n')], 't')]),
        client_body('new', [([('noteid', 'n'), ('text', 'attr text')], 'body')]),
        client_body('new', [([('noteid', 'n'), ('onclick', 'evil()'),
                              ('id', 'x'), ('NOTEID', 'y')], 't')]),
        client_body('new', [(ATTRS, '  \t padded 　\x1c ')]),
        client_body('new', [(ATTRS, 'café ♥ \U0001f600')]),
        client_body('new', [(ATTRS, '&amp;&lt;&gt;&quot;&apos;&#233;&#x2665;')]),
        client_body('new', [(ATTRS, 'line1\r\nline2\rline3')]),
        client_body('new', [([('noteid', 'a\tb\nc\r\nd &#10; e')], 't')]),
        client_body('new', [(ATTRS, '<![CDATA[<b>cdata</b>]]>')]),
        client_body('new', [(ATTRS, 'text<![CDATA[cdata]]>more')]),
        client_body('new', [(ATTRS, '<!-- comment -->text')]),
        client_body('new', [(ATTRS, '<b>bold</b>')]),
        client_body('new', [(ATTRS, '<?pi data?>')]),
        "<workspace name='new' nextNoteNum='1'><note noteid='n'/></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='n'></note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='n'><b>x</b></note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='n'><!--c--></note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='a'><note noteid='b'>inner</note></note></workspace>",
        "<workspace name='new' nextNoteNum='1'><group><note noteid='a'>deep</note></group></workspace>",
        "<root><workspace name='new' nextNoteNum='1'><note noteid='a'>x</note></workspace></root>",
        "<root><note noteid='outside'>x</note><workspace name='new' nextNoteNum='1'><note noteid='a'>x</note></workspace></root>",
        "<workspace name='new' nextNoteNum='1'><note noteid='a'>1</note></workspace><!-- trailing -->",
        "<workspace name='first' nextNoteNum='1'><workspace name='second' nextNoteNum='2'><note noteid='a'>x</note></workspace></workspace>",
        "<workspace nextNoteNum='1'><note noteid='a'>no name</note></workspace>",
        "<workspace name='new'><note noteid='a'>no num</note></workspace>",
        "<other/>",
        "",
        "not xml",
        "<workspace name='new' nextNoteNum='1'>",
        "<workspace name='new' name='dup' nextNoteNum='1'></workspace>",
        "<workspace name='new' nextNoteNum='1'>&undefined;</workspace>",
        "<?xml version='1.0' encoding='utf-8'?><workspace name='new' nextNoteNum='1'><note noteid='a'>x</note></workspace>",
        "<?xml version='1.0' encoding='iso-8859-1'?><workspace name='new' nextNoteNum='1'><note noteid='a'>caf\xe9</note></workspace>",
        "﻿<workspace name='new' nextNoteNum='1'><note noteid='a'>bom</note></workspace>",
        "<!DOCTYPE workspace [<!ENTITY e 'expanded'>]><workspace name='new' nextNoteNum='1'><note noteid='a'>&e;</note></workspace>",
        "<ns:workspace xmlns:ns='u' name='new' nextNoteNum='1'><note noteid='a'>x</note></ns:workspace>",
        "<workspace name='new' nextNoteNum='1'><x:note xmlns:x='u' noteid='a'>x</x:note><note x:noteid='b' xmlns:x='u'>y</note></workspace>",
        "<WORKSPACE name='new' nextNoteNum='1'></WORKSPACE>",
        "  <workspace name='new' nextNoteNum='1'></workspace>  ",
        "<!DOCTYPE w [<!ENTITY e 'a&#233;&#x2665;'>]><workspace name='n&e;' nextNoteNum='1'><note noteid='&e;&lt;&#9;x'>&e;</note></workspace>",
        "<workspace name='new' nextNoteNum='1'><?xml version='1.0'?></workspace>",
        "<?xml version='1.0' encoding='us-ascii'?><workspace name='new' nextNoteNum='1'><note noteid='a'>x</note></workspace>",
        "<?xml version='1.0' encoding='us-ascii'?><workspace name='new' nextNoteNum='1'><note noteid='a'>\u00e9</note></workspace>",
        "<?xml version='1.0' encoding='utf-16'?><workspace name='new' nextNoteNum='1'></workspace>",
        "<workspace name='new' nextNoteNum='1'><x:note noteid='a'>x</x:note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note x:noteid='a'>x</note></workspace>",
        "<workspace name='new' nextNoteNum='1' xmlns:x='u'><note x:noteid='a' noteid='b'>x</note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid=\"dq\" bgcolor = 'sp' >x</note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='a'>&#xD;&#13;x&#x10FFFF;</note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='a'>&#0;</note></workspace>",
        "<workspace name='new' nextNoteNum='1'><note noteid='a'>\x7f\x85\ud7ff\ue000\ufffd</note></workspace>",
    ]
    for num in ('0', '7', ' 3 ', '+2', '-5', '1_0', '1__0', '0x10', '1.5', '',
                '１', '٣', '99999999999999999999', '9223372036854775807',
                '9223372036854775808', '-9223372036854775808', '　3'):
        bodies.append(client_body('new', [], num))
    return bodies


def response_record(response):
    record = {'status': response.status_int}
    for name in HEADERS:
        value = response.headers.get(name)
        if value is not None:
            record[name] = value
    if response.status_int < 400:
        record['body'] = base64.b64encode(response.body).decode('ascii')
    return record


def stored_state():
    """Workspaces and the newest revision's notes JSON, by workspace key."""
    state = {}
    for w in datastore.Query('Workspace').Run():
        latest = None
        query = datastore.Query('Notes', {'workspaceKey =': w.key()})
        query.Order(('time', datastore.Query.DESCENDING))
        for n in query.Run(limit=1):
            latest = {
                'time': n['time'].strftime('%Y-%m-%d %H:%M:%S'),
                'time_to_delete': n['time_to_delete'].strftime(
                    '%Y-%m-%d %H:%M:%S') if n.get('time_to_delete') else None,
                'notes': _notes_text(n['notesJsonArray']),
            }
        state[w.key().name()] = {
            'name': w['name'],
            'nextNoteNum': w['nextNoteNum'],
            'time': w['time'].strftime('%Y-%m-%d %H:%M:%S'),
            'revisions': query.Count(),
            'latest': latest,
        }
    return state


def _notes_text(blob):
    try:
        return zlib.decompress(blob).decode('ascii')
    except (zlib.error, UnicodeDecodeError):
        return 'raw:' + base64.b64encode(blob).decode('ascii')


class _FixedClock:
    """Stands in for the datetime module in save_handler."""

    class datetime:
        @staticmethod
        def now(tz=None):
            return SAVE_NOW.astimezone(tz) if tz else SAVE_NOW.replace(tzinfo=None)

    timezone = _datetime_module.timezone
    timedelta = _datetime_module.timedelta


def http_cases():
    workspaces, notes = seed()
    tb = testbed.Testbed()
    tb.activate()
    tb.init_datastore_v3_stub()
    tb.init_memcache_stub()
    ndb.get_context().set_cache_policy(False)
    app = webtest.TestApp(main.app, extra_environ={
        'APPLICATION_ID': os.environ['APPLICATION_ID']}, lint=False)

    def reset():
        tb.get_stub(testbed.DATASTORE_SERVICE_NAME).Clear()
        put_seed(workspaces, notes)

    cases = []
    try:
        reset()
        before = stored_state()
        for req in requests():
            response = app.request(
                req['path'], method=req['method'], headers=req['headers'],
                body=req.get('body', '').encode('utf8'), expect_errors=True)
            cases.append({'request': req, 'response': response_record(response)})
        assert stored_state() == before, 'a read-only request wrote'

        for body in save_bodies():
            # A body that declares Latin-1 is sent in Latin-1.
            body = body.encode('latin-1' if 'iso-8859-1' in body else 'utf8')
            reset()
            with mock.patch.object(save_handler, 'datetime', _FixedClock):
                response = app.request('/webnote/save.py', method='POST',
                                       body=body, expect_errors=True)
            after = stored_state()
            changed = {k: v for k, v in after.items() if before.get(k) != v}
            cases.append({
                'request': {'method': 'POST', 'path': '/webnote/save.py',
                            'headers': {}, 'body_b64': base64.b64encode(
                                body).decode('ascii')},
                'response': response_record(response),
                'changed': changed,
            })
    finally:
        tb.deactivate()

    def jsonable_seed():
        return {
            'workspaces': [dict(w, time=w['time'].strftime('%Y-%m-%d %H:%M:%S'))
                           for w in workspaces],
            'notes': [dict(n, time=n['time'].strftime('%Y-%m-%d %H:%M:%S'),
                           blob=base64.b64encode(n['blob']).decode('ascii'))
                      for n in notes],
        }
    return {'save_now': SAVE_NOW.strftime('%Y-%m-%d %H:%M:%S'),
            'seed': jsonable_seed(), 'cases': cases}
