# Общее правило сервера: письма, которые rspamd пометил как спам (X-Spam: Yes), попадают в папку «Спам». Пользователь может вынести их оттуда:
# тогда rspamd учится, что это не спам.
require ["fileinto", "mailbox"];
if header :is "X-Spam" "Yes" {
  fileinto :create "Junk";
  stop;
}
