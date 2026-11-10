# CHANGELOG

Tất cả những thay đổi quan trọng của dự án Owlys Pulse được ghi nhận tại đây.

## [v1.0.1-beta] - 2026-11-04

### Tính năng mới & Cải tiến cốt lõi
- **Global Hotkey phục hồi khẩn cấp:** Đăng ký phím tắt toàn cầu qua WinAPI message thread độc lập. Người dùng có thể nhấn `Ctrl + Alt + Shift + R` hoặc `Ctrl + Shift + F12` bất cứ lúc nào (kể cả khi đang trong game hoặc khi Explorer bị tắt) để dừng Boost và khôi phục toàn bộ hệ thống ngay lập tức.
- **Watchdog cứu hộ Explorer (Extreme Mode I):** Khi kích hoạt chế độ ẩn Explorer Shell, một tiến trình giám sát độc lập được khởi chạy. Nếu Pulse gặp sự cố hoặc bị đóng đột ngột, Watchdog sẽ tự động gọi lại `explorer.exe` trong vòng 2 giây, đảm bảo không bao giờ bị mất màn hình Desktop.
- **Bảo toàn 100% cấu hình gốc (Zero Config Drift):**
  - Lưu và trả lại chính xác giá trị ban đầu của các thiết lập nguồn điện (`CPMINCORES`, `PROCTHROTTLEMIN`) thay vì gán giá trị mặc định cố định.
  - Snapshot chi tiết các key Multimedia Class Scheduler Service (MMCSS) trong Registry và khôi phục nguyên trạng.
  - Kiểm tra `Process Creation Time` trước khi rã đông, loại bỏ hoàn toàn rủi ro tác động nhầm tiến trình mới nếu Windows tái sử dụng PID cũ.
- **Trải nghiệm người dùng (UX/UI) trực quan:**
  - Hiển thị thông báo chi tiết khi kích hoạt Boost: số lượng tiến trình tạm dừng, lượng RAM working set được giải phóng ước tính và phím tắt khôi phục.
  - Báo cáo kết quả khôi phục sau khi dừng phiên: xác nhận số app rã đông và các dịch vụ/nguồn điện đã trả về nguyên trạng.
  - Sửa lỗi kiểm tra quyền Registry `QUERY_VALUE`, đảm bảo app hoạt động trơn tru với quyền người dùng tiêu chuẩn mà không bị chặn thao tác.

---

## [v1.0.0-beta] - 2026-11-03

- Phiên bản thử nghiệm đầu tiên của Owlys Pulse cho Windows.
- Tạm dừng ứng dụng nền an toàn qua WinAPI `NtSuspendProcess` / `NtResumeProcess`.
- Cơ chế SmartWake nhận diện cửa sổ foreground.
- Hỗ trợ xả Standby List (ISLC), Kernel Timer 0.5ms và Power Plan High Performance.
- Bảng từ điển nhận diện tiến trình tiếng Việt (Process KB).
