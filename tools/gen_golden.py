"""Writes golden files for the Go port's tests, using the Python code.

Usage (from the repo root):
  env/bin/python tools/gen_golden.py

The Go tests read testdata/*.json, so they check against what Python actually
does rather than a reading of it. Rerun this when the Python behavior changes,
and commit the output.
"""

import base64
import datetime
import json
import logging
import os
import sys
import zlib

import pytz
from google.appengine.api import datastore
from google.appengine.api import datastore_types
from google.appengine.ext import ndb
from google.appengine.ext import testbed

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
import models  # noqa: E402

TESTDATA = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), 'testdata')


def parse_load_time(value):
    """Parses the load page's time parameter like Workspace.get_notes_list().

    Returns 'fallback' when get_notes_list() falls back to the latest revision
    (ValueError), 'error' when it raises (a 500), or the UTC time it queries.
    """
    # Same steps as get_notes_list(), which needs a datastore to call directly.
    try:
        dt = datetime.datetime.strptime(value, '%Y-%m-%d %H:%M:%S')
        dt = models.TIMEZONE.localize(dt).astimezone(pytz.utc)
    except ValueError:
        return 'fallback'
    except Exception:
        return 'error'
    return format_time(dt)


def format_time(dt):
    # strftime('%Y') doesn't pad years before 1000.
    return '%04d-%02d-%02d %02d:%02d:%02d' % (
        dt.year, dt.month, dt.day, dt.hour, dt.minute, dt.second)


def load_time_cases():
    values = [
        '2017-12-30 12:00:00',
        '2017-07-04 12:30:15',
        # strptime accepts unpadded and space-padded fields.
        '2017-1-5 1:2:3',
        '2017-01- 5 01:02:03',
        '2017-01-5 01:02:03',
        # Field ranges and date validity.
        '2017-13-01 00:00:00',
        '2017-00-01 00:00:00',
        '2017-02-29 00:00:00',
        '2016-02-29 00:00:00',
        '2017-04-31 00:00:00',
        '2017-12-31 24:00:00',
        '2017-12-31 23:60:00',
        '2017-12-31 23:59:60',
        '2017-12-31 23:59:61',
        '2017-12-31 23:59:62',
        '0000-01-01 00:00:00',
        '0001-01-01 00:00:00',
        '0001-01-01 23:59:59',
        '0001-01-02 00:00:00',
        '9999-12-30 23:59:59',
        '9999-12-31 00:00:00',
        '9999-12-31 18:59:59',
        '9999-12-31 19:00:00',
        '9999-12-31 23:59:59',
        # Not the format.
        '',
        'not a time',
        '2017-12-30',
        '2017-12-30 12:00',
        '2017-12-30 12:00:00 ',
        ' 2017-12-30 12:00:00',
        '2017-12-30T12:00:00',
        '2017/12/30 12:00:00',
        '17-12-30 12:00:00',
        '20171-12-30 12:00:00',
        '2017-12-30 12:00:000',
        '2017-012-30 12:00:00',
        '+2017-12-30 12:00:00',
        '-2017-12-30 12:00:00',
        '2017-12-30 12:00:00.5',
        # The separator between date and time is \s+.
        '2017-12-30  12:00:00',
        '2017-12-30\t12:00:00',
        '2017-12-30\n12:00:00',
        '2017-12-30 \t 12:00:00',
        '2017-12-3012:00:00',
    ]
    # Characters accepted as whitespace (all of them) or as digits (ASCII
    # only: the Go port doesn't accept other scripts' digits, which depend on
    # the Unicode version).
    for i in range(0x110000):
        c = chr(i)
        if i < 128 or c.isspace():
            values.append('2017-12-30' + c + '12:00:00')
        if i < 128:
            values.append('2017-12-30 12:00:0' + c)
    # Every half hour around each DST transition, plus ordinary times.
    tz = models.TIMEZONE
    for year in range(2000, 2031):
        for month in (3, 11):
            for day in range(1, 15):
                for minute in range(0, 5 * 60, 30):
                    values.append('%04d-%02d-%02d %02d:%02d:00' % (
                        year, month, day, minute // 60, minute % 60))
            values.append('%04d-%02d-%02d 01:59:59' % (year, month, 8))
    for year in range(1880, 1975, 7):
        values.append('%04d-06-15 12:00:00' % year)
        values.append('%04d-01-15 12:00:00' % year)
    seen = set()
    cases = []
    for value in values:
        if value not in seen:
            seen.add(value)
            cases.append([value, parse_load_time(value)])
    return cases


def localtime_cases():
    """UTC times and their Workspace.localtime_str()."""
    cases = []
    times = []
    for year in range(2000, 2031):
        for month in (3, 11):
            for day in range(1, 15):
                start = datetime.datetime(year, month, day, 4, 0, 0)
                for minutes in range(0, 5 * 60, 30):
                    times.append(start + datetime.timedelta(minutes=minutes))
    times.append(datetime.datetime(1, 1, 1, 12, 0, 0))
    times.append(datetime.datetime(1900, 1, 1, 0, 0, 0))
    times.append(datetime.datetime(9999, 12, 31, 23, 59, 59))
    for time in times:
        workspace = models.Workspace.create('a', 0, time)
        cases.append([format_time(time),
                      workspace.localtime_str()])
    return cases


def notes_encoding_cases():
    """Notes (as ordered key/value pairs) and the JSON ndb stores for them.

    The stored blob is zlib.compress() of this JSON.
    """
    every_char = ''.join(chr(i) for i in range(0x300) if i != 0x7f)
    lists = [
        [],
        [[['id', 'note0'], ['xPos', '10'], ['text', 'hello']]],
        # Order is the XML attribute order; 'text' comes last unless the note
        # also has a text attribute.
        [[['noteid', 'x']], [['text', ''], ['id', 'note1'], ['bgcolor', '#ffff99']]],
        [[['text', every_char + '\x7f\u2028\u2029\ufeff\U0001f600\U0010ffff']]],
        [[['text', '%u5D9%E9 caf\u00e9 <b>&amp;</b> "quoted" \\ /']]],
    ]
    prop = models.Notes.notesJsonArray
    cases = []
    for notes in lists:
        value = [dict(note) for note in notes]
        cases.append([notes, prop._to_base_type(value).decode('ascii')])
    return cases


def notes_blob_cases():
    """Stored notesJsonArray blobs and what get_notes_list() returns for them.

    Each case is {"name", "blob" (base64), "meaning22", "result"}, where result
    is {"value": ...}, {"logged": true, "value": ...} when Python logs an error,
    or {"error": "ExceptionName"} when it raises (a 500).
    """
    def z(text):
        return zlib.compress(text.encode('utf8'))

    blobs = [
        ('zlib list', z('[{"id":"note0","text":"caf\\u00e9"}]')),
        ('zlib list, spaces', z(' [ {"id" : "note0"} ] \n')),
        ('zlib raw UTF-8', z('[{"text":"caf\u00e9 \u2603"}]')),
        ('zlib dict', z('{"id":"note0"}')),
        ('zlib null', z('null')),
        ('zlib string', z('"text"')),
        ('zlib number', z('1.5e3')),
        ('zlib empty list', z('[]')),
        ('zlib not JSON', z('not notes')),
        ('zlib empty', z('')),
        ('zlib invalid UTF-8', zlib.compress(b'["\x80"]')),
        ('zlib UTF-8 BOM', zlib.compress(b'\xef\xbb\xbf[]')),
        ('zlib NaN', z('[NaN]')),
        ('zlib duplicate keys', z('[{"id":"a","id":"b"}]')),
        ('zlib lone surrogate escape', z('["\\ud800"]')),
        ('zlib trailing garbage', z('[]') + b'garbage'),
        ('zlib truncated', z('[{"id":"note0"}]')[:-3]),
        ('zlib level 9', zlib.compress(b'[{"id":"x"}]', 9)),
        ('raw JSON', b'[{"id": "note0"}]'),
        ('raw JSON dict', b'{"id": "note0"}'),
        ('raw not JSON', b'not notes'),
        ('raw invalid UTF-8', b'\x80 not notes'),
        ('raw empty', b''),
        ('raw UTF-8 BOM JSON', b'\xef\xbb\xbf[]'),
    ]
    tb = testbed.Testbed()
    tb.activate()
    tb.init_datastore_v3_stub()
    tb.init_memcache_stub()
    ndb.get_context().set_cache_policy(False)
    cases = []
    try:
        time = datetime.datetime(2017, 12, 31)
        for i, (name, blob) in enumerate(blobs):
            for meaning22 in (False, True):
                workspace = models.Workspace.create('w%d' % i, 1, time)
                workspace.put()
                if meaning22:
                    key = put_compressed_notes(workspace, time, blob)
                else:
                    entity = datastore.Entity('Notes')
                    entity['workspaceKey'] = workspace.key.to_old_key()
                    entity['time'] = time
                    entity['notesJsonArray'] = datastore_types.Blob(blob)
                    entity.set_unindexed_properties(['notesJsonArray'])
                    key = datastore.Put(entity)
                got = datastore.Get(key)["notesJsonArray"]
                assert got == blob, (name, meaning22, got[:30], blob[:30])
                cases.append({
                    'name': name,
                    'blob': base64.b64encode(blob).decode('ascii'),
                    'meaning22': meaning22,
                    'result': notes_list_result(workspace),
                })
                datastore.Delete([key, workspace.key.to_old_key()])
    finally:
        tb.deactivate()
    return cases


def put_compressed_notes(workspace, time, blob):
    """Puts a Notes entity whose notesJsonArray is blob, marked the way ndb
    marks a compressed JsonProperty (meaning 22 in the Cloud Datastore API).

    Putting through the model would decompress, parse and re-serialize the
    value, so this patches the entity's protobuf instead.
    """
    pb = models.Notes(workspaceKey=workspace.key, time=time,
                      notesJsonArray=[])._to_pb()
    for prop in pb.raw_property:
        if prop.name == 'notesJsonArray':
            assert prop.meaning_uri == 'ZLIB'
            prop.value.stringValue = blob

    class Raw:
        def _to_pb(self):
            return pb
    key = ndb.get_context()._conn.put([Raw()])[0].to_old_key()
    # The low-level API doesn't know the marker, so it reads the raw blob.
    assert datastore.Get(key)['notesJsonArray'] == blob
    return key


def notes_list_result(workspace):
    handler = _ErrorCounter()
    logging.getLogger().addHandler(handler)
    try:
        value = workspace.get_notes_list(None)
    except Exception as e:  # noqa: BLE001
        return {'error': type(e).__name__}
    finally:
        logging.getLogger().removeHandler(handler)
    try:
        json.dumps(value, allow_nan=False)
    except ValueError:
        return {'error': 'not representable (NaN)'}
    except TypeError:
        # The page's json.dumps() fails, so it's a 500 too.
        return {'error': 'returns %s' % type(value).__name__}
    result = {'value': value}
    if handler.count:
        result['logged'] = True
    return result


class _ErrorCounter(logging.Handler):
    def __init__(self):
        super().__init__(level=logging.ERROR)
        self.count = 0

    def emit(self, record):
        self.count += 1


def write(name, value):
    path = os.path.join(TESTDATA, name)
    with open(path, 'w') as f:
        json.dump(value, f, indent=0, ensure_ascii=True)
        f.write('\n')
    print('wrote', path)


def main():
    os.makedirs(TESTDATA, exist_ok=True)
    write('load_time.json', load_time_cases())
    write('localtime.json', localtime_cases())
    write('notes_encoding.json', notes_encoding_cases())
    write('notes_blobs.json', notes_blob_cases())


if __name__ == '__main__':
    main()
