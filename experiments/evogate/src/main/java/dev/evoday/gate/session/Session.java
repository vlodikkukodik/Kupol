package dev.evoday.gate.session;

import dev.evoday.gate.storage.Account;
import net.kyori.adventure.bossbar.BossBar;
import org.bukkit.scheduler.BukkitTask;

public final class Session {

    final String name;
    final String ip;
    Account account;
    State state;

    String captcha;
    int captchaLeft;
    int loginLeft;
    int secondsLeft;
    // идёт проверка пароля в другом потоке
    boolean busy;
    BukkitTask timer;
    BossBar bar;
    int secondsTotal;

    Session(String name, String ip, Account account) {
        this.name = name;
        this.ip = ip;
        this.account = account;
    }

    public State state() {
        return state;
    }

    public boolean isDone() {
        return state == State.DONE;
    }
}
