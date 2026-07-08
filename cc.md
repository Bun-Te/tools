LOW CPU REGEN (recommended)
cd /Users/microstore/Desktop/frontie/stak/math-sdk-main && source env/bin/activate && rm -rf games/0_0_checkmate_chaos/library/\* && /usr/bin/time -p nice -n 10 python3 games/0_0_checkmate_chaos/regenerate_publish.py 2>&1 | tee /tmp/cc_regen.log

# Lowest impact (slowest): keep UI responsive while regen runs

# cd /Users/microstore/Desktop/frontie/stak/math-sdk-main && source env/bin/activate && rm -rf games/0_0_checkmate_chaos/library/\* && /usr/bin/time -p nice -n 15 python3 games/0_0_checkmate_chaos/regenerate_publish.py 2>&1 | tee /tmp/cc_regen.log

# ETA quick checks while running

# grep -E "Creating books for|Batch|Running production optimization|Finished creating books in|ALL DONE" /tmp/cc_regen.log | tail -n 20

# pgrep -fl "python3 games/0_0_checkmate_chaos/regenerate_publish.py" && echo "regen still running" || echo "regen finished"

# After finish, wall-clock duration from time(1)

# grep -E "^real" /tmp/cc_regen.log

# Show status of all output folders (run this first)

./cc_regen.sh status

# Regenerate the MISSING grand_slam variant (fast — multihunt only)

./cc_regen.sh playerfeel

# Regenerate specific profiles

./cc_regen.sh playerfeel golden_tempo grand_slam

# Full all-modes regen (very slow, heavy CPU)

./cc_regen.sh full

===========

lsof -ti:7754 | xargs kill -9

=========

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../math-sdk-main/games/0_0_checkmate_chaos/library/publish_files_upload_prod -port 7754

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../math-sdk-main/games/0_0_checkmate_chaos/library/publish_files_v8 -port 7754

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../math-sdk-main/games/0_0_checkmate_chaos/library/publish_files_v32 -port 7754

==========

cd /Users/microstore/Desktop/frontie/stak/tools/frontend
pnpm dev --port 7750

US
http://localhost:3002/?rgs_url=http://localhost:7754&sessionID=default-session&social=true&currency=USD

Global
http://localhost:3002/?rgs_url=http://localhost:7754&sessionID=default-session&social=false&currency=USD

Replay
http://localhost:3002/?replay=true&game=checkmate_chaos&version=v23&mode=base&event=4&rgs_url=http://localhost:7754&sessionID=default-session&currency=USD&amount=1&lang=en

Storybook
/?&rgs_url=http://localhost:7754&sessionID=default-session

Event storybook
&replay=true&game=checkmate_chaos&version=1&mode=base&event=8
