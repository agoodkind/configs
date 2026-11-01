length > 0 and all(.[]; .verificationResult.statement.subject | any(.[]; .name == $asset and .digest.sha256 == $sha256))
