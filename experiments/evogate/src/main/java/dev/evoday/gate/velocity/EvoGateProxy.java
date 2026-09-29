package dev.evoday.gate.velocity;

import com.google.inject.Inject;
import com.velocitypowered.api.command.CommandSource;
import com.velocitypowered.api.event.Subscribe;
import com.velocitypowered.api.event.command.CommandExecuteEvent;
import com.velocitypowered.api.event.connection.DisconnectEvent;
import com.velocitypowered.api.event.connection.PluginMessageEvent;
import com.velocitypowered.api.event.player.KickedFromServerEvent;
import com.velocitypowered.api.event.player.ServerPreConnectEvent;
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent;
import com.velocitypowered.api.proxy.Player;
import com.velocitypowered.api.proxy.ProxyServer;
import com.velocitypowered.api.proxy.ServerConnection;
import com.velocitypowered.api.proxy.messages.MinecraftChannelIdentifier;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.minimessage.MiniMessage;

import java.util.Locale;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

// часть EvoGate для Velocity: /server, /hub и прочие команды прокси до бэкенда не доходят,
// поэтому до входа их блокирует прокси. Кто вошёл - сообщает бэкенд через канал evogate:auth
public final class EvoGateProxy {

    private static final MinecraftChannelIdentifier CHANNEL = MinecraftChannelIdentifier.create("evogate", "auth");
    private static final Component NOT_LOGGED = MiniMessage.miniMessage().deserialize(
            "<gradient:#F19404:#F14704><b>Proxy</b></gradient> <dark_gray>»</dark_gray> <red>Сначала войдите.</red>");

    private final ProxyServer proxy;
    private final Set<UUID> authed = ConcurrentHashMap.newKeySet();

    @Inject
    public EvoGateProxy(ProxyServer proxy) {
        this.proxy = proxy;
    }

    @Subscribe
    public void onInit(ProxyInitializeEvent event) {
        proxy.getChannelRegistrar().register(CHANNEL);
    }

    private boolean locked(Player player) {
        return !authed.contains(player.getUniqueId());
    }

    @Subscribe(priority = Short.MAX_VALUE)
    public void onPluginMessage(PluginMessageEvent event) {
        if (!event.getIdentifier().equals(CHANNEL)) {
            return;
        }
        // дальше не пересылаем ни в какую сторону; клиент не должен уметь "войти" сам
        event.setResult(PluginMessageEvent.ForwardResult.handled());
        if (!(event.getSource() instanceof ServerConnection server) || event.getData().length != 1) {
            return;
        }
        UUID uuid = server.getPlayer().getUniqueId();
        if (event.getData()[0] == 1) {
            authed.add(uuid);
        } else {
            authed.remove(uuid);
        }
    }

    @Subscribe(priority = Short.MAX_VALUE)
    public void onCommand(CommandExecuteEvent event) {
        CommandSource source = event.getCommandSource();
        if (!(source instanceof Player player) || !locked(player)) {
            return;
        }
        String label = event.getCommand().split(" ", 2)[0].toLowerCase(Locale.ROOT);
        // команды бэкенда (/login, /register) уходят дальше, там их проверяет сам EvoGate
        if (proxy.getCommandManager().hasCommand(label)) {
            event.setResult(CommandExecuteEvent.CommandResult.denied());
            player.sendMessage(NOT_LOGGED);
        }
    }

    // до входа нельзя уйти на другой сервер (первое подключение пропускаем)
    @Subscribe(priority = Short.MIN_VALUE)
    public void onPreConnect(ServerPreConnectEvent event) {
        Player player = event.getPlayer();
        if (locked(player) && player.getCurrentServer().isPresent()) {
            event.setResult(ServerPreConnectEvent.ServerResult.denied());
            player.sendMessage(NOT_LOGGED);
        }
    }

    // кик с сервера входа (капча, таймаут, антибот) - отключаем, а не отправляем
    // на запасной сервер из try, где EvoGate может не быть
    @Subscribe(priority = Short.MIN_VALUE)
    public void onKicked(KickedFromServerEvent event) {
        if (locked(event.getPlayer())) {
            event.setResult(KickedFromServerEvent.DisconnectPlayer.create(
                    event.getServerKickReason().orElse(Component.text("Disconnected"))));
        }
    }

    @Subscribe
    public void onDisconnect(DisconnectEvent event) {
        authed.remove(event.getPlayer().getUniqueId());
    }
}
