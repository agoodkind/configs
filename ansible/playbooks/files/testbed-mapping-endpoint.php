<?php
declare(strict_types=1);

require_once('/usr/local/etc/inc/config.inc');
require_once('/usr/local/etc/inc/util.inc');

use OPNsense\Core\Config;

/** @return array<string, array<string, string>|string> */
function mappingRule(string $uuid, string $source, string $family, string $description): array
{
    return [
        '@attributes' => ['uuid' => $uuid],
        'type' => 'pass',
        'interface' => 'wan',
        'ipprotocol' => $family,
        'statetype' => 'keep state',
        'descr' => $description,
        'direction' => 'in',
        'quick' => '1',
        'protocol' => 'tcp',
        'source' => ['address' => $source],
        'destination' => ['network' => 'wanip', 'port' => '1406'],
    ];
}

$native = Config::getInstance();
try {
    $native->lock();
    $config = parse_config();
    if ($config['interfaces']['wan']['ipaddr'] !== '10.240.240.2') {
        throw new RuntimeException('The mapping endpoint requires the testbed WAN address');
    }
    $desired = [
        mappingRule('6bec49b6-29c8-4b59-aa90-24fd7509cf82', '10.240.205.1', 'inet',
            'MWAN testbed mapping endpoint AT&T'),
        mappingRule('495c5aa5-773b-4a6d-95da-c3b6f8e7331e', '10.241.204.1', 'inet',
            'MWAN testbed mapping endpoint Webpass'),
        mappingRule('0e05f006-8537-41b3-859f-7e48d58072d8', '3d06:bad:b01:200::91', 'inet6',
            'MWAN testbed IPv6 mapping endpoint AT&T'),
        mappingRule('b6d05d7a-e8bb-4537-9c19-a1bfc4b46c3b', '3d06:bad:b01:200::90', 'inet6',
            'MWAN testbed IPv6 mapping endpoint Webpass'),
    ];
    $changed = false;
    foreach ($desired as $rule) {
        $matching = [];
        foreach ($config['filter']['rule'] as $index => $existing) {
            if (isset($existing['@attributes']['uuid']) &&
                $existing['@attributes']['uuid'] === $rule['@attributes']['uuid']) {
                $matching[] = $index;
            }
        }
        if (count($matching) > 1) {
            throw new RuntimeException('The mapping endpoint has duplicate rule identifiers');
        }
        if ($matching === []) {
            $config['filter']['rule'][] = $rule;
            $changed = true;
        } elseif ($config['filter']['rule'][$matching[0]] !== $rule) {
            $config['filter']['rule'][$matching[0]] = $rule;
            $changed = true;
        }
    }
    if ($changed) {
        $saved = write_config('Configure the MWAN testbed mapping endpoint');
        if (!is_array($saved)) {
            throw new RuntimeException('OPNsense could not save the mapping rules');
        }
    }
    $native->unlock();
    $native->forceReload();
    $readback = $native->toArray(listtags());
    foreach ($desired as $rule) {
        $matching = array_filter($readback['filter']['rule'],
            static function (array $existing) use ($rule): bool {
                return isset($existing['@attributes']['uuid']) &&
                    $existing['@attributes']['uuid'] === $rule['@attributes']['uuid'];
            });
        if (count($matching) !== 1 || array_values($matching)[0] !== $rule) {
            throw new RuntimeException('The saved mapping rule differs from its intent');
        }
    }
    echo json_encode(['changed' => $changed, 'rules' => $desired], JSON_THROW_ON_ERROR) . "\n";
} catch (Throwable $error) {
    fwrite(STDERR, 'Mapping endpoint: ' . $error->getMessage() . "\n");
    exit(1);
} finally {
    $native->unlock();
}
