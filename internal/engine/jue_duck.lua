-- jue_duck.lua: jue's ducking envelope. jue writes it into its runtime dir and loads it into
-- the one mpv it spawns, so it lives and dies with that player. It owns volume-gain, a gain
-- stage of its own, so the volume a human set is never touched. jue validates every argument
-- against the same bounds; the checks here only keep a foreign sender from poisoning state.
local PROTOCOL = "1"                   -- jue reads it from user-data before it sends anything
local STEP, FLOOR = 0.02, -96          -- 20 ms ramp tick; volume-gain-min's default
local gain, from, to, t0, len = 1, 1, 1, 0, 0
local ramp, release

-- num is s as a number in [lo, hi], else nil. tonumber takes "nan", "inf" and "1e309";
-- one of those in gain would leave every later unduck at the floor. NaN fails v == v and
-- the bounds are finite, so this is also the finiteness check.
local function num(s, lo, hi)
    local v = tonumber(s)
    if v and v == v and v >= lo and v <= hi then return v end
end

local function apply(g)
    gain = g
    mp.set_property_number("volume-gain", g > 0 and math.max(FLOOR, 20 * math.log(g) / math.log(10)) or FLOOR)
end

ramp = mp.add_periodic_timer(STEP, function()
    local p = math.min((mp.get_time() - t0) / len, 1)
    apply(from + (to - from) * (1 - math.cos(math.pi * p)) / 2)   -- raised cosine: no corner at either end
    if p >= 1 then ramp:kill() end
end)
ramp:kill()

-- glide moves gain to target over ms: at once when ms is 0, not at all when it is there or on its way.
local function glide(target, ms)
    if ms == 0 then
        ramp:kill()
        from, to = target, target
        apply(target)
        return
    end
    if target == to and (gain == target or ramp:is_enabled()) then return end
    ramp:kill()
    from, to, t0, len = gain, target, mp.get_time(), ms / 1000
    ramp:resume()
end

-- duck <level 0-100> <hold s> <attack ms> <release ms>: down now, back up hold seconds from now.
mp.register_script_message("duck", function(level, hold, attack, back)
    level, hold, attack, back = num(level, 0, 100), num(hold, 0, 30), num(attack, 0, 2000), num(back, 0, 4000)
    if not (level and hold and hold > 0 and attack and back) then
        return mp.msg.warn("duck: arguments out of range, ignored")
    end
    glide(level / 100, attack)
    if release then release:kill() end
    release = mp.add_timeout(hold, function() release = nil; glide(1, back) end)
end)

-- unduck <release ms>: back up now.
mp.register_script_message("unduck", function(back)
    back = num(back, 0, 2000)
    if not back then
        return mp.msg.warn("unduck: argument out of range, ignored")
    end
    if release then release:kill(); release = nil end
    glide(1, back)
end)

-- Battery unplug guard & idle reaper:
-- 1) If mpv was running on AC Power and the Mac switches to Battery Power, quit immediately
--    so background playback never drains the battery after unplugging. Starting play while
--    already on Battery Power sets on_ac = false and is left alone.
-- 2) If mpv stays idle (idle-active == true) for IDLE_MAX seconds, quit so the daemon never
--    lingers indefinitely after a queue finishes.
local IDLE_MAX, POWER_POLL = 600, 10
local on_ac, power_inflight, power_timer, idle_timer = nil, false, nil, nil

local function on_power_sample(out)
    if type(out) ~= "string" then return end
    if out == "reset" then
        on_ac = nil
    elseif out:find("'AC Power'", 1, true) then
        on_ac = true
    elseif out:find("'Battery Power'", 1, true) then
        if on_ac then
            mp.msg.info("jue_duck: unplugged from AC power; quitting mpv")
            mp.command("quit")
        elseif on_ac == nil then
            on_ac = false
        end
    end
end

local function poll_power()
    if power_inflight then return end
    power_inflight = true
    mp.command_native_async({
        name = "subprocess",
        args = {"/usr/bin/pmset", "-g", "batt"},
        playback_only = false,
        capture_stdout = true,
        capture_stderr = true,
    }, function(ok, res)
        power_inflight = false
        if ok and res and res.status == 0 then
            on_power_sample(res.stdout)
        elseif power_timer then
            power_timer:kill()
        end
    end)
end

mp.register_script_message("power-sample", on_power_sample)
poll_power()
power_timer = mp.add_periodic_timer(POWER_POLL, poll_power)

mp.observe_property("idle-active", "bool", function(_, idle)
    if idle_timer then idle_timer:kill(); idle_timer = nil end
    if idle then
        idle_timer = mp.add_timeout(IDLE_MAX, function()
            idle_timer = nil
            mp.msg.info("jue_duck: idle timeout reached; quitting mpv")
            mp.command("quit")
        end)
    end
end)

mp.set_property("user-data/jue/duck-helper", PROTOCOL)   -- last: every handler above is in place
