# This filter exits successfully only if at least one statement exists and every statement
# has a subject with the exact $asset name and $sha256 digest.
length > 0 and all(.[]; .verificationResult.statement.subject | any(.[]; .name == $asset and .digest.sha256 == $sha256))
