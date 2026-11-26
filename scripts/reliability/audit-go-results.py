#!/usr/bin/env python3
"""Fail closed on failed, missing or unexpectedly skipped real-DB test results."""
import argparse
import collections
import json
from pathlib import Path


def audit(path, mode):
    counts = collections.Counter()
    output = collections.defaultdict(list)
    skips, failures, passed = [], [], set()
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        key = (event.get("Package", ""), event.get("Test", ""))
        action = event.get("Action")
        if action == "output":
            output[key].append(event.get("Output", ""))
        if action == "fail":
            failures.append({"package": key[0], "test": key[1]})
        if not key[1] or action not in ("pass", "fail", "skip"):
            continue
        counts[action] += 1
        if action == "pass":
            passed.add(key[1])
        if action == "skip":
            reason = "".join(output[key]).strip()
            explanation = None
            if any(marker in reason for marker in ("opt-in", "set RUN_", "set NOTIFY_PERF=1")):
                explanation = "Dedicated optional load/A-B experiment; excluded from deterministic regression gate."
            skips.append({"package": key[0], "test": key[1], "reason": reason, "explanation": explanation})
            if explanation is None:
                failures.append({"package": key[0], "test": key[1], "error": "unapproved skip"})
    if counts["pass"] < 100:
        failures.append({"error": "missing or truncated test stream (<100 passing tests)"})
    required = {"TestCareSOSOutboxAuditAndState", "TestCareAdminTransitionsAndFamilyIsolation", "TestCareReceiptRequiresTrustedGatewaySignature"}
    if mode == "mysql":
        required.add("TestFamilyAddressBackfillMySQL")
        required.add("TestResidentBirthdateBackfillMySQL")
        required.add("TestHealthProjectionProtectionMySQL")
        required.add("TestProtectedBaselineMatchesLegacyMySQL")
        required |= {"TestProtectedLegacyVitalsMySQL", "TestProtectedLegacyVitalsRuleFallbackMySQL"}
        required |= {"TestMySQLImageCiphertextRotation", "TestMySQLImageRotationPreservesHTTPDownload", "TestMySQLCoordinatedBackupRestore"}
        required.add("TestMySQLMigrationRejectsUnknownHistoryBeforeDDL")
        required.add("TestSocialCommentViewerWithoutPhoneMySQL")
        required |= {"TestResidentCareProfileProtectedMySQL", "TestResidentProfileAtomicFailureMySQL", "TestResidentProfilePhoneAmbiguityMySQL", "TestResidentProfileRotationMySQL", "TestResidentProfileBackfillMySQL", "TestResidentProfilePreflightMySQL", "TestResidentProfileBackfillLocksMySQL", "TestResidentProfileCommandMySQL"}
        required |= {"TestGuardianContactProtectedMySQL", "TestGuardianContactAtomicFailureMySQL", "TestGuardianContactRotationMySQL", "TestGuardianContactFirstWritersMySQL", "TestGuardianContactBackfillMySQL", "TestGuardianContactPreflightMySQL", "TestGuardianContactBackfillLocksMySQL", "TestGuardianContactCommandMySQL"}
        required |= {"TestSocialPhonesAreProtectedMySQL", "TestSocialPhoneBackfillMySQL", "TestSocialPhoneBackfillLocksMySQL", "TestSocialPhonePreflightMySQL", "TestSocialPhoneFailClosedMySQL", "TestSocialPhoneConcurrentVotesMySQL", "TestSocialPhoneCommandMySQL", "TestSocialPhoneImageRollbackMySQL", "TestSocialPhoneRekeyMySQL"}
        required |= {"TestCapabilitiesRollbackAndReapply", "TestHighCardinalityHardCaps256Clients", "TestKeyRotationAndRetirementFailClosed", "TestRekeyMySQL", "TestAdministratorPhoneRekeyMySQL", "TestFamilyPhoneBackfillMySQL", "TestFamilyPhoneCommandMySQL", "TestFamilyPhoneRecipientsLegacyAndProtectedMySQL", "TestProtectedFamilyLifecycleMySQL", "TestMySQLImageStorageAndLegacyImport", "TestMySQLImageTwoInstancesAndRestoredPayload", "TestCareFullHTTPChainAfterRealGatewayTimeout"}
    for name in sorted(required - passed):
        failures.append({"test": name, "error": "required test did not pass"})
    return {"mode": mode, "counts": dict(counts), "skips": skips, "failures": failures, "accepted": not failures}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("log")
    parser.add_argument("--mode", required=True, choices=("mysql",))
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    result = audit(args.log, args.mode)
    Path(args.output).write_text(json.dumps(result, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    print(json.dumps({"mode": args.mode, "counts": result["counts"], "accepted": result["accepted"], "failures": result["failures"]}))
    raise SystemExit(0 if result["accepted"] else 1)
