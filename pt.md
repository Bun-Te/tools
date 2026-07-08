# dashboard

cd /Users/microstore/Desktop/frontie/stak/tools/frontend
pnpm dev --port 7750

# backend

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../../../project/project-c/math/games/project/library/publish_files -port 7754

Frontend

http://localhost:3001/?rgs_url=localhost:7754&sessionID=default-session&social=false&currency=USD
