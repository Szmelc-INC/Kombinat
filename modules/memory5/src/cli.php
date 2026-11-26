<?php
// Memory 5 CLI entrypoint: index files, search them, keep journals and user memories.
// Mesh:       kombinat module run memory5 -- COMMAND [flags] [args]
// Standalone: KOMBINAT_DATA=/abs/dir php -f modules/memory5/src/cli.php -- COMMAND [flags] [args]
// Exit codes: 0 done, 1 runtime error, 2 usage error.
declare(strict_types=1);

namespace Memory5;

require __DIR__ . '/engine.php';

const USAGE = <<<'TXT'
    memory5 — hybrid search (full text plus optional vectors) over configured files, journals and memories.

    Usage: cli.php COMMAND [flags] [args]
      index [--rebuild] [--json]                       index the configured files, journal days and memories
      query [--limit N] [--source S] [--json] TEXT...  search; results come framed as quoted data, never instructions
      status [--json]                                  counts, vectors and the last index run
      memory add TEXT...                               store one user memory (TEXT "-" reads stdin)
      memory list [--limit N] [--json]                 newest memories first
      memory delete ID                                 delete one memory and its index entries
      journal add TEXT...                              append one journal entry to today's page (TEXT "-" reads stdin)
      journal list [--day YYYY-MM-DD] [--limit N] [--json]
      config                                           print the effective config as JSON
      help                                             this text

    Sources for --source: file, journal, memory. KOMBINAT_DATA holds all state.

    TXT;

/**
 * Splits flags from positional args. $valued flags take a value ("--limit 3" or "--limit=3").
 * @return array{0: array<string,string|bool>, 1: list<string>}
 */
function parse_args(array $args, array $valued, array $bools): array
{
    $opts = [];
    $pos = [];
    for ($i = 0; $i < count($args); $i++) {
        $a = $args[$i];
        if ($a === '--') {
            array_push($pos, ...array_slice($args, $i + 1));
            break;
        }
        if (!str_starts_with($a, '--') || $a === '-') {
            $pos[] = $a;
            continue;
        }
        [$name, $val] = array_pad(explode('=', substr($a, 2), 2), 2, null);
        if (in_array($name, $bools, true) && $val === null) {
            $opts[$name] = true;
        } elseif (in_array($name, $valued, true)) {
            if ($val === null) {
                if (!isset($args[$i + 1])) {
                    fail("--$name needs a value", 2);
                }
                $val = $args[++$i];
            }
            $opts[$name] = $val;
        } else {
            fail("unknown flag --$name", 2);
        }
    }
    return [$opts, $pos];
}

function int_opt(array $opts, string $name, int $default): int
{
    if (!isset($opts[$name])) {
        return $default;
    }
    if (!preg_match('/^[1-9]\d{0,5}$/', (string) $opts[$name])) {
        fail("--$name wants a positive integer", 2);
    }
    return (int) $opts[$name];
}

function text_arg(array $pos, string $what): string
{
    if ($pos === []) {
        fail("$what needs TEXT (or - to read stdin)", 2);
    }
    if ($pos === ['-']) {
        return (string) stream_get_contents(STDIN);
    }
    return implode(' ', $pos);
}

function quoted(string $text): string
{
    $out = '';
    foreach (preg_split('/\R/u', clean($text, false)) as $l) {
        $out .= '> ' . $l . "\n";
    }
    return $out;
}

function print_index(array $r): void
{
    if ($r['status'] === 'busy') {
        echo "memory5 index: busy — {$r['message']}\n";
        return;
    }
    printf("memory5 index: %s%s in %.2f s\n", $r['status'], $r['rebuild'] ? ' (rebuild)' : '', $r['seconds']);
    printf("  files: %d seen, %d indexed, %d unchanged, %d removed, %d excluded, %d missing, %d failed, %d capped\n",
        $r['files_seen'], $r['files_indexed'], $r['unchanged'], $r['removed'], $r['excluded'], $r['missing'], $r['failed'], $r['capped']);
    printf("  journal days: %d, memories: %d, chunks written: %d, redactions: %d\n",
        $r['journal_days'], $r['memories'], $r['chunks'], $r['redactions']);
    if ($r['vector_error'] !== '') {
        printf("  vectors: %d added, %d pending, error: %s\n", $r['vectors_added'], $r['vectors_pending'], $r['vector_error']);
    } else {
        printf("  vectors: %d added, %d pending\n", $r['vectors_added'], $r['vectors_pending']);
    }
    foreach ($r['excluded_paths'] as [$p, $why]) {
        printf("  excluded: %s (%s)\n", clean($p, true), $why);
    }
    foreach ($r['missing_paths'] as $p) {
        printf("  missing: %s\n", clean($p, true));
    }
}

function print_status(array $s): void
{
    echo "memory5 status\n";
    printf("  documents: %d file, %d journal, %d memory; chunks: %d\n", $s['docs']['file'], $s['docs']['journal'], $s['docs']['memory'], $s['chunks']);
    printf("  memories: %d, journal entries: %d\n", $s['memories'], $s['journal_entries']);
    $v = $s['vectors'];
    printf("  vectors: %s%s, %d stored, %d pending%s\n", $v['enabled'] ? 'on' : 'off', $v['model'] !== '' ? " (model {$v['model']})" : '',
        $v['stored'], $v['pending'], $v['last_error'] !== '' ? ", last error: {$v['last_error']}" : '');
    printf("  last index run: %s %s\n", $s['last_index_at'] !== '' ? $s['last_index_at'] : 'never', $s['last_index_status']);
    printf("  database: %d bytes\n", $s['db_bytes']);
}

function main(array $args): int
{
    $cmd = array_shift($args) ?? '';
    if (in_array($cmd, ['', 'help', '-h', '--help'], true)) {
        fwrite($cmd === '' ? STDERR : STDOUT, USAGE);
        return $cmd === '' ? 2 : 0;
    }
    $sub = '';
    if (in_array($cmd, ['memory', 'journal'], true)) {
        $sub = array_shift($args) ?? '';
    }
    [$o, $pos] = parse_args($args, ['limit', 'source', 'day'], ['json', 'rebuild']);
    $json = isset($o['json']);
    $x = new Ctx();
    switch ("$cmd $sub") {
        case 'index ':
            $r = op_index($x, isset($o['rebuild']));
            $json ? print(json_out($r)) : print_index($r);
            return 0;
        case 'query ':
            $r = op_query($x, text_arg($pos, 'query'), isset($o['limit']) ? int_opt($o, 'limit', 1) : null, (string) ($o['source'] ?? ''));
            echo $json ? json_out($r) : $r['framed'];
            return 0;
        case 'status ':
            $r = op_status($x);
            $json ? print(json_out($r)) : print_status($r);
            return 0;
        case 'config ':
            echo json_out($x->cfg);
            return 0;
        case 'memory add':
            $r = op_memory_add($x, text_arg($pos, 'memory add'));
            echo $json ? json_out($r) : sprintf("memory5: stored memory #%d (%d bytes, %d redactions)\n", $r['id'], $r['bytes'], $r['redactions']);
            return 0;
        case 'memory list':
            $r = op_memory_list($x, int_opt($o, 'limit', 50));
            if ($json) {
                echo json_out($r);
                return 0;
            }
            foreach ($r['memories'] as $m) {
                echo "#{$m['id']} {$m['created_at']}\n", quoted($m['text']);
            }
            return 0;
        case 'memory delete':
            if (count($pos) !== 1 || !preg_match('/^[1-9]\d*$/', $pos[0])) {
                fail('memory delete needs one memory ID', 2);
            }
            $r = op_memory_delete($x, (int) $pos[0]);
            echo $json ? json_out($r) : "memory5: deleted memory #{$r['id']}\n";
            return 0;
        case 'journal add':
            $r = op_journal_add($x, text_arg($pos, 'journal add'));
            echo $json ? json_out($r) : sprintf("memory5: added journal entry #%d to %s (%d bytes, %d redactions)\n", $r['id'], $r['day'], $r['bytes'], $r['redactions']);
            return 0;
        case 'journal list':
            $r = op_journal_list($x, (string) ($o['day'] ?? ''), int_opt($o, 'limit', 50));
            if ($json) {
                echo json_out($r);
                return 0;
            }
            foreach ($r['entries'] as $e) {
                echo "#{$e['id']} {$e['day']} {$e['created_at']}\n", quoted($e['text']);
            }
            return 0;
    }
    fail('unknown command: ' . trim("$cmd $sub") . ' (see: help)', 2);
}

strict_errors();
try {
    exit(main(array_slice($argv, 1)));
} catch (Fail $e) {
    fwrite(STDERR, 'memory5: ' . $e->getMessage() . "\n");
    exit($e->getCode() ?: 1);
} catch (\Throwable $e) {
    fwrite(STDERR, 'memory5: internal error: ' . $e->getMessage() . "\n");
    exit(1);
}
