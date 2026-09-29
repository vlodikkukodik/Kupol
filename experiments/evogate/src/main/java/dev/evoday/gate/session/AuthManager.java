package dev.evoday.gate.session;

import dev.evoday.gate.EvoGate;
import dev.evoday.gate.antibot.AntiBot;
import dev.evoday.gate.captcha.CaptchaImage;
import dev.evoday.gate.storage.Account;
import dev.evoday.gate.storage.AccountRepo;
import dev.evoday.gate.util.Messages;
import dev.evoday.gate.util.Passwords;
import net.kyori.adventure.bossbar.BossBar;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.format.NamedTextColor;
import net.kyori.adventure.title.Title;
import org.bukkit.Bukkit;
import org.bukkit.Location;
import org.bukkit.World;
import org.bukkit.configuration.ConfigurationSection;
import org.bukkit.configuration.file.FileConfiguration;
import org.bukkit.entity.Player;
import org.bukkit.potion.PotionEffect;
import org.bukkit.potion.PotionEffectType;

import java.time.Duration;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Consumer;
import java.util.logging.Level;

public final class AuthManager {

    private final EvoGate plugin;
    private final Map<UUID, Session> sessions = new ConcurrentHashMap<>();
    // аккаунты, загруженные в AsyncPlayerPreLoginEvent, забираются в join
    private final Map<UUID, Optional<Account>> preloaded = new ConcurrentHashMap<>();

    private final SkyHold sky;

    public AuthManager(EvoGate plugin) {
        this.plugin = plugin;
        this.sky = new SkyHold(plugin);
    }

    private FileConfiguration cfg() {
        return plugin.getConfig();
    }

    private Messages msg() {
        return plugin.messages();
    }

    public Session session(Player player) {
        return sessions.get(player.getUniqueId());
    }

    public boolean isLocked(Player player) {
        Session s = sessions.get(player.getUniqueId());
        return s != null && !s.isDone();
    }

    public void preload(UUID uuid, Account account) {
        preloaded.put(uuid, Optional.ofNullable(account));
    }

    public void dropPreload(UUID uuid) {
        preloaded.remove(uuid);
    }

    // игроки, которые были онлайн при включении плагина (/reload) - считаем вошедшими
    public void adopt(Player player) {
        Session s = new Session(player.getName(), ip(player), null);
        s.state = State.DONE;
        sessions.put(player.getUniqueId(), s);
        plugin.proxy().send(player, true);
    }

    public void onJoin(Player player) {
        Optional<Account> loaded = preloaded.remove(player.getUniqueId());
        if (loaded == null) {
            player.kick(msg().raw("kick-loading"));
            return;
        }
        // слепота и точка в небе сохраняются в данных игрока - могли остаться с прошлого захода
        clearBlindness(player);
        sky.land(player);
        Account account = loaded.orElse(null);
        Session s = new Session(player.getName(), ip(player), account);
        sessions.put(player.getUniqueId(), s);

        long sessionMs = cfg().getLong("auth.session-minutes") * 60_000L;
        if (account != null && sessionMs > 0 && s.ip.equals(account.lastIp())
                && System.currentTimeMillis() - account.lastLogin() < sessionMs) {
            s.state = State.DONE;
            plugin.proxy().send(player, true);
            msg().send(player, "session-ok");
            return;
        }

        AntiBot antiBot = plugin.antiBot();
        if (!antiBot.gravityEnabled()) {
            lock(player, true);
            afterCheck(player, s, true);
            return;
        }
        lock(player, false);
        s.state = State.CHECK;
        antiBot.startGravity(player, passed -> {
            if (!passed && "kick".equalsIgnoreCase(cfg().getString("antibot.gravity-check.fail-action"))) {
                plugin.getLogger().info(player.getName() + " failed the gravity check");
                player.kick(msg().raw("kick-bot"));
                return;
            }
            // проверка закончилась - поднимаем обратно и держим в воздухе
            sky.lift(player, true);
            afterCheck(player, s, passed);
        });
    }

    private void afterCheck(Player player, Session s, boolean passed) {
        String mode = cfg().getString("captcha.mode", "new").toLowerCase(Locale.ROOT);
        boolean captcha = mode.equals("always")
                || (mode.equals("new") && s.account == null)
                || plugin.antiBot().isUnderAttack()
                || !passed;
        if (captcha) {
            startCaptcha(player, s);
        } else {
            startAuth(player, s);
        }
    }

    // сколько игроков с этого IP сейчас висит без входа
    public int unauthedFrom(String ip) {
        int n = 0;
        for (Session s : sessions.values()) {
            if (!s.isDone() && s.ip.equals(ip)) {
                n++;
            }
        }
        return n;
    }

    public void onQuit(Player player) {
        Session s = sessions.remove(player.getUniqueId());
        if (s == null) {
            return;
        }
        stopTimer(s);
        plugin.antiBot().cancel(player);
        plugin.captcha().hide(player);
        if (!s.isDone()) {
            clearBlindness(player);
            sky.land(player);
        }
    }

    public void shutdown() {
        for (Player p : Bukkit.getOnlinePlayers()) {
            Session s = sessions.get(p.getUniqueId());
            if (s != null && !s.isDone()) {
                plugin.antiBot().cancel(p);
                plugin.captcha().hide(p);
                unlock(p, null);
            }
        }
        sessions.values().forEach(this::stopTimer);
        sessions.clear();
    }

    // ---------- капча ----------

    private void startCaptcha(Player player, Session s) {
        s.state = State.CAPTCHA;
        s.captcha = CaptchaImage.randomCode(Math.max(3, Math.min(CaptchaImage.MAX_LENGTH, cfg().getInt("captcha.length", 5))));
        s.captchaLeft = cfg().getInt("captcha.attempts", 3);
        plugin.captcha().show(player, s.captcha);
        startTimer(player, s, cfg().getInt("captcha.timeout", 60), "kick-captcha");
        prompt(player, s);
    }

    public void onCaptchaInput(Player player, String input) {
        Session s = session(player);
        if (s == null || s.state != State.CAPTCHA) {
            return;
        }
        if (input.trim().equalsIgnoreCase(s.captcha)) {
            plugin.captcha().hide(player);
            msg().send(player, "captcha-ok");
            startAuth(player, s);
            return;
        }
        s.captchaLeft--;
        if (s.captchaLeft <= 0) {
            player.kick(msg().raw("kick-captcha"));
            return;
        }
        msg().send(player, "captcha-wrong", "attempts", s.captchaLeft);
        // новый код после ошибки, чтобы не подбирали
        s.captcha = CaptchaImage.randomCode(s.captcha.length());
        plugin.captcha().show(player, s.captcha);
    }

    // ---------- вход / регистрация ----------

    private void startAuth(Player player, Session s) {
        s.state = s.account == null ? State.REGISTER : State.LOGIN;
        // на капче слепоты нет, иначе панель не видно
        if (cfg().getBoolean("auth.hide-unauthed", true)) {
            player.addPotionEffect(new PotionEffect(PotionEffectType.BLINDNESS, PotionEffect.INFINITE_DURATION, 0, false, false, false));
        }
        s.loginLeft = cfg().getInt("auth.attempts", 5);
        startTimer(player, s, cfg().getInt("auth.timeout", 90), "kick-timeout");
        prompt(player, s);
    }

    public void login(Player player, String password) {
        Session s = session(player);
        if (!checkCanAuth(player, s)) {
            return;
        }
        if (s.account == null) {
            msg().send(player, "not-registered");
            return;
        }
        s.busy = true;
        Account account = s.account;
        CompletableFuture.supplyAsync(() -> Passwords.verify(password, account.hash()))
                .whenComplete((ok, error) -> sync(player, p -> {
                    s.busy = false;
                    if (error != null) {
                        plugin.getLogger().log(Level.SEVERE, "login check failed", error);
                        msg().send(p, "error");
                        return;
                    }
                    if (!ok) {
                        s.loginLeft--;
                        if (s.loginLeft <= 0) {
                            p.kick(msg().raw("kick-attempts"));
                        } else {
                            msg().send(p, "wrong-password", "attempts", s.loginLeft);
                        }
                        return;
                    }
                    plugin.db().async(c -> {
                        AccountRepo.touchLogin(c, s.name, s.ip);
                        return null;
                    });
                    finish(p, s, "login-ok");
                }));
    }

    public void register(Player player, String password, String repeat) {
        Session s = session(player);
        if (!checkCanAuth(player, s)) {
            return;
        }
        if (s.account != null) {
            msg().send(player, "already-registered");
            return;
        }
        if (!password.equals(repeat)) {
            msg().send(player, "passwords-mismatch");
            return;
        }
        if (!checkPassword(player, password)) {
            return;
        }
        s.busy = true;
        int ipLimit = cfg().getInt("auth.max-accounts-per-ip", 3);
        UUID uuid = player.getUniqueId();
        plugin.db().async(c -> {
            if (ipLimit > 0 && AccountRepo.countByIp(c, s.ip) >= ipLimit) {
                return RegisterResult.IP_LIMIT;
            }
            String hash = Passwords.hash(password);
            if (!AccountRepo.insert(c, uuid, s.name, hash, s.ip)) {
                return RegisterResult.TAKEN;
            }
            s.account = new Account(uuid, s.name, hash, s.ip, s.ip, System.currentTimeMillis());
            return RegisterResult.OK;
        }).whenComplete((result, error) -> sync(player, p -> {
            s.busy = false;
            if (error != null) {
                plugin.getLogger().log(Level.SEVERE, "register failed", error);
                msg().send(p, "error");
            } else if (result == RegisterResult.IP_LIMIT) {
                msg().send(p, "ip-limit");
            } else if (result == RegisterResult.TAKEN) {
                msg().send(p, "already-registered");
            } else {
                finish(p, s, "register-ok");
            }
        }));
    }

    private enum RegisterResult { OK, IP_LIMIT, TAKEN }

    public void changePassword(Player player, String oldPassword, String newPassword) {
        Session s = session(player);
        if (s == null || !s.isDone() || s.account == null) {
            msg().send(player, "not-logged");
            return;
        }
        if (s.busy) {
            msg().send(player, "busy");
            return;
        }
        if (!checkPassword(player, newPassword)) {
            return;
        }
        s.busy = true;
        Account account = s.account;
        plugin.db().async(c -> {
            if (!Passwords.verify(oldPassword, account.hash())) {
                return null;
            }
            String hash = Passwords.hash(newPassword);
            AccountRepo.setHash(c, s.name, hash);
            return hash;
        }).whenComplete((hash, error) -> sync(player, p -> {
            s.busy = false;
            if (error != null) {
                plugin.getLogger().log(Level.SEVERE, "change password failed", error);
                msg().send(p, "error");
            } else if (hash == null) {
                msg().send(p, "wrong-password", "attempts", "-");
            } else {
                s.account = new Account(account.uuid(), account.name(), hash, account.regIp(), s.ip, 0);
                msg().send(p, "password-changed");
            }
        }));
    }

    public void logout(Player player) {
        Session s = session(player);
        if (s == null || !s.isDone()) {
            msg().send(player, "not-logged");
            return;
        }
        plugin.db().async(c -> {
            AccountRepo.resetSession(c, s.name);
            return null;
        });
        msg().send(player, "logged-out");
        lock(player, true);
        startAuth(player, s);
    }

    public boolean forceLogin(Player player) {
        Session s = session(player);
        if (s == null || s.isDone()) {
            return false;
        }
        plugin.antiBot().cancel(player);
        plugin.captcha().hide(player);
        finish(player, s, "login-ok");
        return true;
    }

    // аккаунт удалили из админки - если игрок онлайн, выкидываем
    public void onUnregistered(Player player) {
        Session s = session(player);
        if (s != null) {
            s.account = null;
        }
        player.kick(msg().raw("kick-unregistered"));
    }

    private boolean checkCanAuth(Player player, Session s) {
        if (s == null || s.isDone()) {
            msg().send(player, "already-logged");
            return false;
        }
        if (s.state == State.CHECK) {
            msg().send(player, "wait-check");
            return false;
        }
        if (s.state == State.CAPTCHA) {
            msg().send(player, "wait-captcha");
            return false;
        }
        if (s.busy) {
            msg().send(player, "busy");
            return false;
        }
        return true;
    }

    public boolean checkPassword(Player player, String password) {
        int min = cfg().getInt("auth.password-min", 6);
        int max = cfg().getInt("auth.password-max", 32);
        if (password.length() < min || password.length() > max) {
            msg().send(player, "password-length", "min", min, "max", max);
            return false;
        }
        if (password.equalsIgnoreCase(player.getName())) {
            msg().send(player, "password-equals-name");
            return false;
        }
        return true;
    }

    private void finish(Player player, Session s, String messageKey) {
        stopTimer(s);
        s.state = State.DONE;
        plugin.proxy().send(player, true);
        unlock(player, afterLoginTarget(messageKey.equals("register-ok")));
        player.clearTitle();
        player.sendActionBar(Component.empty());
        msg().send(player, messageKey);
    }

    // ---------- блокировка ----------

    // null - вернуть туда, где стоял
    private Location afterLoginTarget(boolean registered) {
        if (!"location".equalsIgnoreCase(cfg().getString("after-login.mode", "back"))) {
            return null;
        }
        if (cfg().getBoolean("after-login.only-after-register") && !registered) {
            return null;
        }
        ConfigurationSection l = cfg().getConfigurationSection("after-login.location");
        World world = l == null ? null : Bukkit.getWorld(l.getString("world", "world"));
        if (world == null) {
            plugin.getLogger().warning("after-login.location: world not found, teleporting back instead");
            return null;
        }
        return new Location(world, l.getDouble("x"), l.getDouble("y"), l.getDouble("z"),
                (float) l.getDouble("yaw"), (float) l.getDouble("pitch"));
    }

    private void lock(Player player, boolean hover) {
        plugin.proxy().send(player, false);
        sky.lift(player, hover);
        if (!cfg().getBoolean("auth.hide-unauthed", true)) {
            return;
        }
        for (Player other : Bukkit.getOnlinePlayers()) {
            if (other != player) {
                other.hidePlayer(plugin, player);
            }
        }
    }

    // снимаем любую слепоту: до входа игрок ничего не может сделать, так что своей у него быть не может
    private void clearBlindness(Player player) {
        if (player.hasPotionEffect(PotionEffectType.BLINDNESS)) {
            player.removePotionEffect(PotionEffectType.BLINDNESS);
        }
    }

    private void unlock(Player player, Location target) {
        clearBlindness(player);
        sky.land(player, target);
        for (Player other : Bukkit.getOnlinePlayers()) {
            other.showPlayer(plugin, player);
        }
    }

    // зашедшему игроку не показываем тех, кто ещё не вошёл
    public void hideLockedFrom(Player viewer) {
        if (!cfg().getBoolean("auth.hide-unauthed", true)) {
            return;
        }
        for (Player other : Bukkit.getOnlinePlayers()) {
            if (other != viewer && isLocked(other)) {
                viewer.hidePlayer(plugin, other);
            }
        }
    }

    // ---------- таймер и подсказки ----------

    private void startTimer(Player player, Session s, int seconds, String kickKey) {
        stopTimer(s);
        s.secondsLeft = seconds;
        s.secondsTotal = Math.max(1, seconds);
        // после капчи таймер запускается заново - на вход снова полное время
        s.bar = BossBar.bossBar(barTitle(s), 1f, BossBar.Color.YELLOW, BossBar.Overlay.PROGRESS);
        player.showBossBar(s.bar);
        s.timer = Bukkit.getScheduler().runTaskTimer(plugin, () -> {
            if (!player.isOnline()) {
                stopTimer(s);
                return;
            }
            s.secondsLeft--;
            if (s.secondsLeft <= 0) {
                stopTimer(s);
                player.kick(msg().raw(kickKey));
                return;
            }
            s.bar.name(barTitle(s));
            s.bar.progress(Math.max(0f, Math.min(1f, (float) s.secondsLeft / s.secondsTotal)));
            if (s.secondsLeft <= 10) {
                s.bar.color(BossBar.Color.RED);
            }
            if (s.secondsLeft % 10 == 0) {
                prompt(player, s);
            }
        }, 20L, 20L);
    }

    private Component barTitle(Session s) {
        String key = switch (s.state) {
            case CAPTCHA -> "bossbar-captcha";
            case REGISTER -> "bossbar-register";
            default -> "bossbar-login";
        };
        return msg().raw(key, "seconds", s.secondsLeft);
    }

    private void stopTimer(Session s) {
        if (s.timer != null) {
            s.timer.cancel();
            s.timer = null;
        }
        if (s.bar != null) {
            BossBar bar = s.bar;
            s.bar = null;
            Bukkit.getOnlinePlayers().forEach(p -> p.hideBossBar(bar));
        }
    }

    private void prompt(Player player, Session s) {
        String key = switch (s.state) {
            case CAPTCHA -> "captcha";
            case REGISTER -> "register";
            case LOGIN -> "login";
            case CHECK, DONE -> null;
        };
        if (key == null) {
            return;
        }
        player.showTitle(Title.title(msg().raw(key + "-title"), msg().raw(key + "-subtitle"),
                Title.Times.times(Duration.ZERO, Duration.ofSeconds(11), Duration.ofMillis(300))));
        msg().send(player, key + "-prompt", "attempts", s.captchaLeft);
    }

    private void sync(Player player, Consumer<Player> action) {
        Bukkit.getScheduler().runTask(plugin, () -> {
            if (player.isOnline()) {
                action.accept(player);
            }
        });
    }

    private static String ip(Player player) {
        var address = player.getAddress();
        return address == null ? "" : address.getAddress().getHostAddress();
    }
}
