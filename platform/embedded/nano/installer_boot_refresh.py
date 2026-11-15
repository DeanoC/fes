"""Refresh only the staged FIT after checking all independent expected sources.

The caller owns the console and supplies fresh recovery/kit/source admission.
This operation never activates an image or proves native boot or persistence.
"""
from installer_recovery import CATALOG_PURPOSES, _source_inputs, _read_member
from service_trace import require
from upload import UploadSession, destination, sha, valid_payload_length


def refresh_staged_boot(session, payloads, receipt, *, previous_boot, retained_records):
    require(type(receipt) is dict and not receipt, 'fresh boot-refresh receipt required')
    require(isinstance(session, UploadSession), 'owned UploadSession required')
    receipt.update(scope='nano-staged-boot-refresh-v1',status='checking',before={},after={},
                   boot_upload={},sd_write_attempted=False,activation=False,
                   fresh_recovery_boot_proven=False,cid_verified=False,persistence_verified=False,
                   unchanged_sources_verified=False)
    failure = None
    try:
        require(not session.retired and not session.verified, 'boot refresh requires a fresh session')
        expected, retained = _source_inputs(payloads,CATALOG_PURPOSES,retained_records)
        require(bool(retained), 'explicit prior installed record required')
        require(type(previous_boot) is bytes and valid_payload_length('boot',len(previous_boot)),
                'bounded immutable previous staged boot required')
        require(sha(previous_boot) != sha(expected['boot']), 'staged boot is already the requested image')
        token = session.token
        preserved = [('installed-record',digest,payload,'FES/INSTALLED/'+digest+'.JSN')
                     for digest,payload in retained.items()]
        preserved.extend((purpose,purpose,expected[purpose],destination(purpose))
                         for purpose in CATALOG_PURPOSES if purpose != 'boot')
        def read_all(phase, boot, ledger):
            for purpose,key,payload,target in [*preserved,('boot','boot',boot,destination('boot'))]:
                row = {'commands':[]}
                receipt[phase][key] = row
                _read_member(session,payload,row,purpose,target,token,ledger)
        read_all('before',previous_boot,{})
        receipt['status'] = 'uploading-boot'
        row = receipt['boot_upload']
        row.update(commands=[],payload_bytes=len(expected['boot']),payload_sha256=sha(expected['boot']))
        session.upload(expected['boot'],row,purpose='boot')
        ledger = {'boot':(len(expected['boot']),sha(expected['boot']))}
        require(row['status'] == 'verified' and row['upload_session'] == token == session.token and
                row['purpose'] == 'boot' and row['destination'] == destination('boot') and
                row['payload_bytes'] == len(expected['boot']) and row['payload_sha256'] == sha(expected['boot']) and
                session.verified == ledger and all(row[k] is True for k in
                ('sd_write_attempted','download_verified','sd_readback_verified','card_verified',
                 'boot_files_unchanged','recovery_manifest_unchanged')),
                'boot upload lacks complete owned write/readback proof')
        require([v['stage'] for v in row['memory_observations']] == ['admit','before-sd-write','complete'],
                'boot upload scratch ownership proof differs')
        read_all('after',expected['boot'],ledger)
        receipt.update(status='verified',unchanged_sources_verified=True,
                       previous_boot_sha256=sha(previous_boot),boot_sha256=sha(expected['boot']),
                       retained_record_sha256=list(retained),purposes=list(CATALOG_PURPOSES))
    except BaseException as error:
        failure = error
        receipt.update(status='failed',error=str(error))
        raise
    finally:
        receipt['sd_write_attempted'] = receipt['boot_upload'].get('sd_write_attempted',False)
        try:
            session.close()
            receipt['session_closed'] = True
        except BaseException as error:
            receipt.update(status='failed',session_closed=False,session_close_error=str(error))
            if failure is None:
                raise
            failure.add_note('boot-refresh session closure also failed: '+str(error))
    return receipt
