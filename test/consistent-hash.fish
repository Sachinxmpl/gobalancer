#!/usr/bin/env fish
#
# Demonstrates consistent hashing in LoadGate.
#
# The L7 proxy hashes the CLIENT IP (RemoteAddr) to pick a backend, so from
# localhost every request is the same key -> same backend (that's stickiness,
# not a bug). On Linux the whole 127.0.0.0/8 range is loopback, so we can send
# from many different source IPs with `curl --interface 127.0.0.x` and watch
# the keys spread across backends -- while each IP stays glued to one backend.
#
# Prereqs:
#   - configl7.example.yaml has `balancer: consistent_hash`
#   - three backends running on 9001/9002/9003:
#       NAME=b1 PORT=9001 go run ./testbackend &
#       NAME=b2 PORT=9002 go run ./testbackend &
#       NAME=b3 PORT=9003 go run ./testbackend &
#   - proxy running:  ./bin/loadgate run -c configl7.example.yaml
#

set URL http://127.0.0.1:8080/

if not curl -s -o /dev/null --max-time 2 $URL
    echo "proxy not reachable at $URL -- start it first" >&2
    exit 1
end

echo "== distribution: 50 distinct client IPs =="

set results
for i in (seq 2 51)
    set ip 127.0.0.$i
    set body (curl -s --interface $ip $URL)
    set results $results $body
end

printf '%s\n' $results | sort | uniq -c | sort -rn
echo

echo "== stickiness: each client hit 5x, should never change =="

for ip in 127.0.0.2 127.0.0.7 127.0.0.23 127.0.0.44 127.0.0.99
    set hits
    for n in (seq 1 5)
        set hits $hits (curl -s --interface $ip $URL | string trim)
    end
    # collapse the 5 hits to unique values: 1 unique == perfectly sticky
    set uniq (printf '%s\n' $hits | sort -u | string join ', ')
    printf '  %-14s -> %s\n' $ip "$uniq"
end
echo

echo "Reading it:"
echo "  Part A -- keys land on several backends (not all one): the ring spreads load."
echo "  Part B -- each client shows exactly ONE backend across 5 hits: sticky routing."
echo "  (Note: consistent_hash ignores weights, so distribution won't match 10:1:1.)"
