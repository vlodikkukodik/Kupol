package dev.evoday.gate.util;

import dev.evoday.gate.EvoGate;
import org.bukkit.entity.Player;
import org.bukkit.event.EventHandler;
import org.bukkit.event.Listener;
import org.bukkit.event.player.PlayerRegisterChannelEvent;

// сообщает прокси (Velocity), вошёл ли игрок: /server и прочие команды прокси
// до бэкенда не доходят, поэтому блокировать их может только прокси
public final class ProxyBridge implements Listener {

    public static final String CHANNEL = "evogate:auth";

    private final EvoGate plugin;

    public ProxyBridge(EvoGate plugin) {
        this.plugin = plugin;
    }

    public void install() {
        plugin.getServer().getMessenger().registerOutgoingPluginChannel(plugin, CHANNEL);
        plugin.getServer().getPluginManager().registerEvents(this, plugin);
    }

    public void uninstall() {
        plugin.getServer().getMessenger().unregisterOutgoingPluginChannel(plugin, CHANNEL);
    }

    // без прокси канал никто не регистрирует, и Bukkit сообщение просто не отправит
    public void send(Player player, boolean authed) {
        player.sendPluginMessage(plugin, CHANNEL, new byte[]{(byte) (authed ? 1 : 0)});
    }

    // прокси регистрирует канал чуть позже входа - тогда и досылаем текущее состояние
    @EventHandler
    public void onRegister(PlayerRegisterChannelEvent event) {
        if (CHANNEL.equals(event.getChannel())) {
            var s = plugin.auth().session(event.getPlayer());
            send(event.getPlayer(), s != null && s.isDone());
        }
    }
}
