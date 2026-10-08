require ["vnd.dovecot.pipe", "copy", "imapsieve", "environment", "variables"];
if environment :matches "imap.mailbox" "*" {
  set "mailbox" "${1}";
}
# из «Спама» в корзину — это просто удаление, а не «не спам»
if string "${mailbox}" "Trash" {
  stop;
}
if environment :matches "imap.user" "*" {
  set "username" "${1}";
}
pipe :copy "rspamd-learn-ham.sh" [ "${username}" ];
