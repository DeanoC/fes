"""Read-only staged-source checks before a separately admitted discovery boot.

This owns and closes one UploadSession, but never opens/closes the console or
proves a fresh recovery boot, card CID, installed catalogue or persistence.
"""
import json

from service_trace import _unique, require
from upload import UploadSession, destination, sha, valid_payload_length

PURPOSES = ('boot','package','metadata','firmware','menu-package','menu-metadata',
            'menu-selection','selection','ui-catalog','install-manifest')
CATALOG_PURPOSES = ('boot','package','metadata','firmware','package-alt','metadata-alt',
                    'firmware-alt','tape','menu-package','menu-metadata','menu-selection',
                    'selection','ui-catalog','install-manifest')


def _source_inputs(payloads, purposes, retained_records):
    require(type(purposes) is tuple and purposes in (PURPOSES,CATALOG_PURPOSES),
            'independently selected fixed source role set required')
    require(type(payloads) is dict and set(payloads) == set(purposes), 'exact staged source set required')
    expected = {}
    for purpose in purposes:
        payload = payloads[purpose]
        require(type(payload) is bytes and valid_payload_length(purpose,len(payload)),
                'bounded immutable staged payload required')
        expected[purpose] = payload
    require(type(retained_records) is tuple and len(retained_records) <= 32,
            'bounded explicit retained-record tuple required')
    retained = {}
    for payload in retained_records:
        require(type(payload) is bytes and 0 < len(payload) <= 8192,
                'bounded immutable retained record required')
        digest = sha(payload)
        require(digest not in retained, 'duplicate expected retained record')
        retained[digest] = payload
    def invalid(_):
        raise ValueError('nonfinite bootstrap field')
    selection = json.loads(expected['selection'],object_pairs_hook=_unique,parse_constant=invalid)
    catalogue = json.loads(expected['ui-catalog'],object_pairs_hook=_unique,parse_constant=invalid)
    require(type(selection) is dict and type(catalogue) is dict and
            type(selection.get('format')) is int and type(catalogue.get('format')) is int and
            selection == dict(format=1,programs=[],scope='nano-program-selection-v1') and
            catalogue == dict(format=1,entries=[],scope='nano-tenfoot-catalog-v1'),
            'independently expected bootstrap must remain empty')
    return expected, retained

def _read_member(session, payload, row, purpose, target, token, ledger):
    if purpose == 'installed-record':
        session.read_installed_record(payload,row)
    else:
        session.read_existing(payload,row,purpose=purpose)
    require(row['upload_session'] == token == session.token and session.verified == ledger,
            'staged read adopted another session or upload ledger')
    require(row['status'] == 'verified' and row['purpose'] == purpose and
            row['destination'] == target and
            row['payload_bytes'] == len(payload) and row['payload_sha256'] == sha(payload),
            'staged read differs from independent expected content')
    require(all(row[k] is True for k in ('read_only','fresh_readback','sd_readback_verified',
                                        'boot_files_unchanged','recovery_manifest_unchanged','card_verified')) and
            all(row[k] is False for k in ('sd_write_attempted','activation','cid_verified')) and
            row['identity_scope'] == 'capacity-layout-protected-recovery-files',
            'staged read scope or protected-file proof differs')
    require([v['stage'] for v in row['memory_observations']] == ['admit','complete'],
            'staged read lacks complete scratch ownership checks')


def read_staged_sources(session, payloads, receipt, *, purposes, retained_records=()):
    require(type(receipt) is dict and not receipt, 'fresh staged-read receipt required')
    require(isinstance(session,UploadSession), 'owned UploadSession required')
    receipt.update(scope='nano-read-only-staged-sources-v1',status='checking',files={},retained_records={},
                   read_only=True,sd_write_attempted=False,activation=False,
                   fresh_recovery_boot_proven=False,cid_verified=False,persistence_verified=False,
                   retained_record_bytes_verified=False)
    failure = None
    try:
        require(not session.retired and not session.verified, 'read-only session must not adopt an upload ledger')
        expected, retained = _source_inputs(payloads,purposes,retained_records)
        token = session.token
        reads = [('installed-record',digest,payload,'FES/INSTALLED/'+digest+'.JSN')
                 for digest,payload in retained.items()]
        reads.extend((purpose,purpose,expected[purpose],destination(purpose)) for purpose in purposes)
        for purpose,key,payload,target in reads:
            row = {'commands':[]}
            receipt['retained_records' if purpose == 'installed-record' else 'files'][key] = row
            _read_member(session,payload,row,purpose,target,token,{})
        receipt.update(status='verified',purposes=list(purposes),bootstrap_empty=True,
                       retained_record_bytes_verified=bool(retained))
    except BaseException as error:
        failure = error
        receipt.update(status='failed',error=str(error))
        raise
    finally:
        try:
            session.close()
            receipt['session_closed'] = True
        except BaseException as error:
            receipt.update(status='failed',session_closed=False,session_close_error=str(error))
            if failure is None:
                raise
            failure.add_note('staged-read session closure also failed: '+str(error))
    return receipt
