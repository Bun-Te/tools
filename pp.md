# dashboard

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../math-sdk-main/games/0_0_checkmate_chaos/library/publish_files_upload_prod -port 7754

# backend

cd /Users/microstore/Desktop/frontie/stak/tools/backend
go run ./cmd -dir ../../../../project/project-b/math-sdk-main/games/poly_ponk/library/publish_files -port 7754

Frontend

http://localhost:3001/?rgs_url=localhost:7754&sessionID=default-session&social=false&currency=USD
