/*
  ztncui - ZeroTier network controller UI
  Copyright (C) 2017-2021  Key Networks (https://key-networks.com)
  Licensed under GPLv3 - see LICENSE for details.
*/

const fs = require('fs');
const ipaddr = require('ip-address');
const storage = require('node-persist');
const zt = require('./zt');
const util = require('util');

storage.initSync({ dir: 'etc/storage' });

const networkDescriptionKey = nwid => 'network:' + nwid + ':description';
const pinnedMembersKey = nwid => 'network:' + nwid + ':pinnedMembers';

function memberIPv4ToNumber(ip) {
  if (!ip || ip.indexOf(':') !== -1) return null;
  const parts = ip.split('.').map(part => Number(part));
  if (parts.length !== 4 || parts.some(part => !Number.isInteger(part) || part < 0 || part > 255)) return null;
  return parts.reduce((value, part) => (value * 256) + part, 0);
}

function memberSortIp(member) {
  const assignments = Array.isArray(member.ipAssignments) ? member.ipAssignments : [];
  return assignments.find(ip => memberIPv4ToNumber(ip) !== null) || assignments[0] || '';
}

async function getPinnedMembers(nwid) {
  const pinned = await storage.getItem(pinnedMembersKey(nwid));
  return Array.isArray(pinned) ? pinned : [];
}

function sortMembers(members, pinnedMembers, ipSort) {
  const pinnedSet = new Set(pinnedMembers);
  const direction = ipSort === 'desc' ? -1 : 1;

  return members
    .map((member, index) => {
      member.pinned = pinnedSet.has(member.id);
      member.sortIp = memberSortIp(member);
      member.sortIpNumber = memberIPv4ToNumber(member.sortIp);
      member.originalIndex = index;
      return member;
    })
    .sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      if (ipSort === 'asc' || ipSort === 'desc') {
        const aMissing = a.sortIpNumber === null;
        const bMissing = b.sortIpNumber === null;
        if (aMissing !== bMissing) return aMissing ? 1 : -1;
        if (!aMissing && a.sortIpNumber !== b.sortIpNumber) return (a.sortIpNumber - b.sortIpNumber) * direction;
        return String(a.sortIp).localeCompare(String(b.sortIp), undefined, { numeric: true }) * direction;
      }
      return a.originalIndex - b.originalIndex;
    });
}

async function map_limit(items, limit, fn) {
  const results = new Array(items.length);
  let next = 0;

  async function worker() {
    while (next < items.length) {
      const index = next++;
      results[index] = await fn(items[index], index);
    }
  }

  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
}

async function get_network_with_members(nwid, includePeers = true) {
  const [network, peers, members] = await Promise.all([
    zt.network_detail(nwid),
    includePeers ? zt.peers() : Promise.resolve([]),
    zt.members(nwid)
      .then(member_ids =>
        map_limit(Object.keys(member_ids), 8, id => Promise.all([
          zt.member_detail(nwid, id),
          storage.getItem(id)
        ]))
      ).then(results => results.map(([member, name]) => {
        member.name = name || '';
        return member;
      }))
  ]);
  for (const member of members) {
    member.peer = peers.find(x => x.address === member.address);
  }
  return { network, members };
}

async function get_network_member(nwid, memberid) {
  const [network, member, peer, name] = await Promise.all([
    zt.network_detail(nwid),
    zt.member_detail(nwid, memberid),
    zt.peer(memberid),
    storage.getItem(memberid)
  ]);
  member.name = name || '';
  member.peer = peer;
  return { network, member };
}

// ZT network controller home page
exports.index = async function (req, res) {
  res.redirect('/networks');
};

// Display list of all networks on this ZT network controller
exports.network_list = async function (req, res) {
  const navigate =
  {
    active: 'networks',
  }

  try {
    const [networks, zt_status] = await Promise.all([
      zt.network_list(),
      zt.get_zt_status()
    ]);
    res.render('networks', { title: '此控制器上的网络', navigate: navigate, networks: networks, zt_status: zt_status });
  } catch (err) {
    res.render('networks', { title: '此控制器上的网络', navigate: navigate, error: '检索此控制器上的网络列表时出错: ' + err });
  }
};

// Display detail page for specific network
exports.network_detail = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  try {
    const [
      { network, members },
      zt_address,
      pinnedMembers
    ] = await Promise.all([
      get_network_with_members(req.params.nwid),
      zt.get_zt_address(),
      getPinnedMembers(req.params.nwid)
    ]);
    const ipSort = req.query.ipSort === 'desc' ? 'desc' : (req.query.ipSort === 'asc' ? 'asc' : '');
    res.render('network_detail', { title: '网络 ' + network.name, navigate: navigate, network: network, members: sortMembers(members, pinnedMembers, ipSort), zt_address: zt_address, ipSort: ipSort });
  } catch (err) {
    res.render('network_detail', { title: '网络详细信息', navigate: navigate, error: '解析网络的详细信息时出错 ' + req.params.nwid + ': ' + err });
  }
};

// API endpoint to get member status as JSON (for real-time updates)
exports.network_members_status = async function (req, res) {
  try {
    const [
      { members },
      zt_address,
      pinnedMembers
    ] = await Promise.all([
      get_network_with_members(req.params.nwid),
      zt.get_zt_address(),
      getPinnedMembers(req.params.nwid)
    ]);
    const pinnedSet = new Set(pinnedMembers);

    // Return member status data
    const memberStatus = members.map(member => {
      const isOnline = !!member.peer;
      const lastSeen = member.lastSeen || 0;

      return {
        id: member.id,
        name: member.name,
        authorized: member.authorized,
        ipAssignments: member.ipAssignments,
        online: isOnline,
        latency: member.peer ? member.peer.latency : -1,
        version: member.peer ? member.peer.version : null,
        paths: member.peer && member.peer.paths ? member.peer.paths.map(p => p.address) : [],
        isController: member.id == zt_address,
        lastSeen: lastSeen,
        pinned: pinnedSet.has(member.id)
      };
    });

    res.json({ success: true, members: memberStatus });
  } catch (err) {
    console.error('获取成员状态时出错:', err);
    res.status(500).json({ success: false, error: '获取成员状态失败' });
  }
};

// Display Network create form on GET
exports.network_create_get = function (req, res) {
  const navigate =
  {
    active: 'add_network',
  }

  res.render('network_create', { title: '创建网络', navigate: navigate });
};

// Handle Network create on POST
exports.network_create_post = async function (req, res) {
  const navigate =
  {
    active: 'add_network',
  }

  req.checkBody('name', '需要网络名称').notEmpty();

  req.sanitize('name').escape();
  req.sanitize('name').trim();

  const errors = req.validationErrors();

  let name = { name: req.body.name };

  if (errors) {
    res.render('network_create', { title: '创建网络', navigate: navigate, name: name, errors: errors });
    return;
  } else {
    try {
      const network = await zt.network_create(name);
      res.redirect('/network/' + network.nwid + '/v4AssignMode');
    } catch (err) {
      res.render('network_detail', { title: '创建网络 - 错误', navigate: navigate, error: '创建网络时出错 ' + name.name });
    }
  }
};

// Display Network delete form on GET
exports.network_delete_get = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  try {
    const network = await zt.network_detail(req.params.nwid);
    res.render('network_delete', {
      title: '删除网络', navigate: navigate,
      nwid: req.params.nwid, network: network
    });
  } catch (err) {
    res.render('network_delete', { title: '删除网络', navigate: navigate, error: '解析网络时出错 ' + req.params.nwid + ': ' + err });
  }
};

// Handle Network delete on POST
exports.network_delete_post = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  try {
    const network = await zt.network_delete(req.params.nwid);
    await storage.removeItem(networkDescriptionKey(req.params.nwid));
    await storage.removeItem(pinnedMembersKey(req.params.nwid));
    res.render('network_delete', { title: '删除网络', navigate: navigate, network: network });
  } catch (err) {
    res.render('network_delete', { title: '删除网络', navigate: navigate, error: '删除网络时候出错 ' + req.params.nwid + ': ' + err });
  }
};

// Network object GET
exports.network_object = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const network = await zt.network_detail(req.params.nwid);
    navigate.whence = '/network/' + network.nwid;
    res.render(req.params.object, { title: req.params.object, navigate: navigate, network: network }, function (err, html) {
      if (err) {
        if (err.message.indexOf('Failed to lookup view') !== -1) {
          return res.render('not_implemented', { title: req.params.object, navigate: navigate, network: network });
        }
        throw err;
      }
      res.send(html);
    });
  } catch (err) {
    res.render(req.params.object, { title: req.params.object, navigate: navigate, error: '解析网络的详细信息时出错 ' + req.params.nwid + ': ' + err });
  }
}

// Handle Network rename form on POST
exports.name = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  req.checkBody('name', '需要网络名称').notEmpty();
  req.sanitize('name').escape();
  req.sanitize('name').trim();

  const errors = req.validationErrors();

  let name = { name: req.body.name };

  if (errors) {
    console.error("网络名称验证错误", errors);
  } else {
    try {
      const network = await zt.network_object(req.params.nwid, name);
    } catch (err) {
      console.error("重命名网络时出错 " + req.params.nwid, err);
    }
  }
  res.redirect('/network/' + req.params.nwid);
};

exports.network_details_get = async function (req, res) {
  const navigate = { active: 'networks', whence: '/network/' + req.params.nwid };

  try {
    const [network, description] = await Promise.all([
      zt.network_detail(req.params.nwid),
      storage.getItem(networkDescriptionKey(req.params.nwid))
    ]);
    res.render('network_details', {
      title: '网络详情', navigate: navigate, network: network,
      description: description || ''
    });
  } catch (err) {
    res.render('network_details', {
      title: '网络详情', navigate: navigate,
      error: '读取网络详情时出错 ' + req.params.nwid + ': ' + err
    });
  }
};

exports.network_details_post = async function (req, res) {
  const navigate = { active: 'networks', whence: '/network/' + req.params.nwid };
  const name = String(req.body.name || '').trim();
  const description = String(req.body.description || '').trim();
  const errors = [];

  if (!name) errors.push({ msg: '网络名称不能为空' });
  if (name.length > 128) errors.push({ msg: '网络名称不能超过 128 个字符' });
  if (description.length > 1000) errors.push({ msg: '网络描述不能超过 1000 个字符' });

  try {
    let network = await zt.network_detail(req.params.nwid);
    if (errors.length) {
      return res.render('network_details', {
        title: '网络详情', navigate: navigate, network: network,
        description: description, form: { name: name }, errors: errors
      });
    }

    network = await zt.network_object(req.params.nwid, { name: name });
    if (description) await storage.setItem(networkDescriptionKey(req.params.nwid), description);
    else await storage.removeItem(networkDescriptionKey(req.params.nwid));

    res.render('network_details', {
      title: '网络详情', navigate: navigate, network: network,
      description: description, message: '网络详情已保存'
    });
  } catch (err) {
    res.render('network_details', {
      title: '网络详情', navigate: navigate,
      error: '保存网络详情时出错 ' + req.params.nwid + ': ' + err
    });
  }
};

// ipAssignmentPools POST
exports.ipAssignmentPools = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  req.sanitize('ipRangeStart').trim();
  req.sanitize('ipRangeEnd').trim();

  req.checkBody('ipRangeStart', '需要IP起始地址').notEmpty();
  req.checkBody('ipRangeStart', 'IP起始地址需要有效的IPv4或IPv6地址').isIP();
  req.checkBody('ipRangeEnd', '需要IP结束地址').notEmpty();
  req.checkBody('ipRangeEnd', 'IP结束地址需要有效的IPv4或IPv6地址').isIP();

  const errors = req.validationErrors();

  const ipAssignmentPool =
  {
    ipRangeStart: req.body.ipRangeStart,
    ipRangeEnd: req.body.ipRangeEnd
  };

  if (errors) {
    try {
      const network = await zt.network_detail(req.params.nwid);
      navigate.whence = '/network/' + network.nwid;
      res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, ipAssignmentPool: ipAssignmentPool, network: network, errors: errors });
    } catch (err) {
      res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, error: '解析网络的网络详细信息时出错 ' + req.params.nwid + ': ' + err });
    }
  } else {
    try {
      const network = await zt.ipAssignmentPools(req.params.nwid, ipAssignmentPool, 'add');
      navigate.whence = '/network/' + network.nwid;
      res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, ipAssignmentPool: ipAssignmentPool, network: network });
    } catch (err) {
      res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, error: '应用ip分配池时出错 ' + req.params.nwid + ': ' + err });
    }
  }
}

// Generate RFC4193 ULA IPv6 pool from network ID  
function generateIPv6ULAPool(networkID) {
  const globalId = networkID.substring(0, 10).padEnd(10, '0');
  const prefix = 'fd' + globalId.substring(0, 2) + ':' +
    globalId.substring(2, 6) + ':' + globalId.substring(6, 10);

  return {
    subnet: `${prefix}::/48`,
    ipRangeStart: `${prefix}:9993::1`,
    ipRangeEnd: `${prefix}:9993:ffff:ffff:ffff:ffff`
  };
}

// API endpoint to generate IPv6 pool
exports.generateIPv6Pool = async function (req, res) {
  try {
    const nwid = req.params.nwid;
    const pool = generateIPv6ULAPool(nwid);

    res.json({
      success: true,
      pool: pool,
      message: '已生成 RFC4193 ULA IPv6 地址池'
    });
  } catch (err) {
    console.error('生成 IPv6 池时出错:', err);
    res.status(500).json({
      success: false,
      error: '生成 IPv6 池失败'
    });
  }
};

// Generate a random /24 from the RFC1918 private address ranges.
function generateIPv4Pool() {
  const family = Math.floor(Math.random() * 3);
  let first;
  let second;
  if (family === 0) {
    first = 10;
    second = Math.floor(Math.random() * 256);
  } else if (family === 1) {
    first = 172;
    second = 16 + Math.floor(Math.random() * 16);
  } else {
    first = 192;
    second = 168;
  }
  const third = Math.floor(Math.random() * 256);
  const prefix = `${first}.${second}.${third}`;

  return {
    subnet: `${prefix}.0/24`,
    ipRangeStart: `${prefix}.1`,
    ipRangeEnd: `${prefix}.254`
  };
}

// API endpoint to generate IPv4 pool
exports.generateIPv4Pool = async function (req, res) {
  try {
    const pool = generateIPv4Pool();

    res.json({
      success: true,
      pool: pool,
      message: '已从 RFC1918 私有地址范围生成 IPv4 地址池'
    });
  } catch (err) {
    console.error('生成 IPv4 池时出错:', err);
    res.status(500).json({
      success: false,
      error: '生成 IPv4 池失败'
    });
  }
};


isValidPrefix = function (str, max) {
  const num = Math.floor(Number(str));
  return String(num) == str && num >= 0 && num <= max;
}

// routes POST
exports.routes = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  req.checkBody('target', '目标网络是必需的').notEmpty();
  req.sanitize('target').trim();
  req.checkBody('target', '目标网络必须是有效的CIDR格式')
    .custom(value => {
      const parts = value.split('/');
      const ipv4 = new ipaddr.Address4(parts[0]);
      const ipv6 = new ipaddr.Address6(parts[0]);
      let isValidIPv4orIPv6 = false;
      let prefixMax = 32;
      if (ipv4.isValid()) {
        isValidIPv4orIPv6 = true;
      } else {
      }
      if (ipv6.isValid()) {
        isValidIPv4orIPv6 = true;
        prefixMax = 128;
      } else {
      }
      return isValidIPv4orIPv6 && isValidPrefix(parts[1], prefixMax);
    });
  req.checkBody('via', '网关必须是有效的IPv4或IPv6地址').optional({ checkFalsy: true }).isIP();
  req.sanitize('via').escape();
  req.sanitize('via').trim();
  if (!req.body.via) {
    req.body.via = null;
  }

  const errors = req.validationErrors();

  const route =
  {
    target: req.body.target,
    via: req.body.via
  };

  if (errors) {
    try {
      const network = await zt.network_detail(req.params.nwid);
      navigate.whence = '/network/' + network.nwid;
      res.render('routes', { title: '托管路由', navigate: navigate, route: route, network: network, errors: errors });
    } catch (err) {
      res.render('routes', { title: '托管路由', navigate: navigate, error: '解析网络详细信息时出错' });
    }
  } else {
    try {
      const network = await zt.routes(req.params.nwid, route, 'add');
      navigate.whence = '/network/' + network.nwid;
      res.render('routes', { title: '托管路由', navigate: navigate, route: route, network: network });
    } catch (err) {
      res.render('routes', { title: '托管路由', navigate: navigate, error: '为网络添加路由时出错 ' + req.params.nwid + ': ' + err });
    }
  }

}

// route_delete GET
exports.route_delete = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  const route =
  {
    target: req.params.target_ip + '/' + req.params.target_prefix,
    via: null
  };


  try {
    const network = await zt.routes(req.params.nwid, route, 'delete');
    navigate.whence = '/network/' + network.nwid;
    res.render('routes', { title: '托管路由', navigate: navigate, route: route, network: network });
  } catch (err) {
    res.render('routes', { title: '托管路由', navigate: navigate, error: '删除网络的路由时出错 ' + req.params.nwid + ': ' + err });
  }
}

// ipAssignmentPool_delete GET
exports.ipAssignmentPool_delete = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  const ipAssignmentPool =
  {
    ipRangeStart: req.params.ipRangeStart,
    ipRangeEnd: req.params.ipRangeEnd
  };


  try {
    const network = await zt.ipAssignmentPools(req.params.nwid, ipAssignmentPool, 'delete');
    navigate.whence = '/network/' + network.nwid;
    res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, ipAssignmentPool: ipAssignmentPool, network: network });
  } catch (err) {
    res.render('ipAssignmentPools', { title: 'ip分配池', navigate: navigate, error: '删除网络的IP分配池时出错 ' + req.params.nwid + ': ' + err });
  }
}

// private POST
exports.private = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  const private =
  {
    private: req.body.private
  };

  try {
    const network = await zt.network_object(req.params.nwid, private);
    navigate.whence = '/network/' + network.nwid;
    res.render('private', { title: '私有', navigate: navigate, network: network });
  } catch (err) {
    res.render('private', { title: '私有', navigate: navigate, error: '应用专用网络时出错 ' + req.params.nwid + ': ' + err });
  }
}

const toBool = function (v) {
  return v === true || v === 'true' || v === 'on' || v === '1';
};

const isIPv6Value = function (value) {
  return String(value || '').includes(':');
};

const ipv4ToNumber = function (value) {
  const parts = String(value || '').split('.');
  if (parts.length !== 4) return null;
  const octets = parts.map(part => Number(part));
  if (octets.some((octet, index) => !Number.isInteger(octet) || octet < 0 || octet > 255 || String(octet) !== parts[index])) return null;
  return octets[0] * 16777216 + octets[1] * 65536 + octets[2] * 256 + octets[3];
};

const numberToIPv4 = function (value) {
  return [
    Math.floor(value / 16777216) % 256,
    Math.floor(value / 65536) % 256,
    Math.floor(value / 256) % 256,
    value % 256
  ].join('.');
};

const parseIPv4Subnet = function (value) {
  const parts = String(value || '').trim().split('/');
  const address = ipv4ToNumber(parts[0]);
  const prefix = Number(parts[1]);
  if (address === null || !Number.isInteger(prefix) || prefix < 1 || prefix > 30) return null;
  const size = Math.pow(2, 32 - prefix);
  const first = Math.floor(address / size) * size;
  return {
    cidr: numberToIPv4(first) + '/' + prefix,
    first: first,
    last: first + size - 1
  };
};

const replaceLocalRoute = function (routes, protocol, target) {
  const nextRoutes = (routes || []).map(route => Object.assign({}, route));
  const isTargetProtocol = route => isIPv6Value(route.target) === (protocol === 6);
  const index = nextRoutes.findIndex(route => !route.via && isTargetProtocol(route));
  if (target) {
    const route = { target: target, via: null };
    if (index >= 0) nextRoutes[index] = route;
    else nextRoutes.push(route);
  } else if (index >= 0) {
    nextRoutes.splice(index, 1);
  }
  return nextRoutes;
};

// advanced network settings POST (enableBroadcast / multicastLimit / mtu)
exports.advanced = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  const enableBroadcast = toBool(req.body.enableBroadcast);
  const privateNetwork = toBool(req.body.privateNetwork);
  const multicastLimit = Number(req.body.multicastLimit);

  try {
    if (!Number.isInteger(multicastLimit) || multicastLimit < 0) {
      const network = await zt.network_detail(req.params.nwid);
      navigate.whence = '/network/' + network.nwid;
      return res.render('advanced', {
        title: '组播', navigate: navigate, network: network,
        form: { enableBroadcast: enableBroadcast, multicastLimit: req.body.multicastLimit, privateNetwork: privateNetwork },
        errors: [{ msg: '组播接收者上限必须是大于或等于 0 的整数' }]
      });
    }

    const advanced = {
      enableBroadcast: enableBroadcast,
      multicastLimit: multicastLimit,
      private: privateNetwork
    };
    const network = await zt.network_object(req.params.nwid, advanced);
    navigate.whence = '/network/' + network.nwid;
    res.render('advanced', { title: '组播', navigate: navigate, network: network, message: '组播设置已保存' });
  } catch (err) {
    res.render('advanced', { title: '组播', navigate: navigate, error: '保存组播设置时出错 ' + req.params.nwid + ': ' + err });
  }
}

// member noAutoAssignIps POST (JSON)
exports.member_no_auto_assign = async function (req, res) {
  const object =
  {
    noAutoAssignIps: toBool(req.body.noAutoAssignIps)
  };

  try {
    await zt.member_object(req.params.nwid, req.params.id, object);
    res.json({ ok: true });
  } catch (err) {
    res.status(500).json({ ok: false, error: '设置 noAutoAssignIps 时出错: ' + err });
  }
}

// v4AssignMode POST
exports.v4AssignMode = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const current = await zt.network_detail(req.params.nwid);
    const subnet = parseIPv4Subnet(req.body.ipv4Subnet);
    const autoAssign = toBool(req.body.zt);
    const poolStart = String(req.body.poolStart || '').trim();
    const poolEnd = String(req.body.poolEnd || '').trim();
    const errors = [];

    if (!subnet) errors.push({ msg: '请输入有效的 IPv4 子网，例如 10.147.17.0/24' });
    if (autoAssign) {
      const start = ipv4ToNumber(poolStart);
      const end = ipv4ToNumber(poolEnd);
      if (start === null || end === null) errors.push({ msg: '自动分配范围必须是有效的 IPv4 地址' });
      else if (start > end) errors.push({ msg: 'IPv4 起始地址不能大于结束地址' });
      else if (subnet && (start <= subnet.first || end >= subnet.last)) errors.push({ msg: 'IPv4 自动分配范围必须位于子网的可用地址范围内' });
    }

    if (errors.length) {
      return res.render('v4AssignMode', {
        title: 'IPv4 分配', navigate: navigate, network: current, errors: errors,
        form: { ipv4Subnet: req.body.ipv4Subnet, poolStart: poolStart, poolEnd: poolEnd, autoAssign: autoAssign }
      });
    }

    const pools = (current.ipAssignmentPools || []).filter(pool => isIPv6Value(pool.ipRangeStart));
    if (autoAssign) pools.push({ ipRangeStart: poolStart, ipRangeEnd: poolEnd });
    const object = {
      routes: replaceLocalRoute(current.routes, 4, subnet.cidr),
      ipAssignmentPools: pools,
      v4AssignMode: { zt: autoAssign }
    };
    const network = await zt.network_object(req.params.nwid, object);
    navigate.whence = '/network/' + network.nwid;
    res.render('v4AssignMode', { title: 'IPv4 分配', navigate: navigate, network: network, message: 'IPv4 分配设置已保存' });
  } catch (err) {
    res.render('v4AssignMode', { title: 'IPv4 分配', navigate: navigate, error: '保存 IPv4 分配设置时出错 ' + req.params.nwid + ': ' + err });
  }
}

// v6AssignMode POST
exports.v6AssignMode = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const current = await zt.network_detail(req.params.nwid);
    const customAssign = toBool(req.body.zt);
    const subnetValue = String(req.body.ipv6Subnet || '').trim();
    const poolStart = String(req.body.poolStart || '').trim();
    const poolEnd = String(req.body.poolEnd || '').trim();
    const errors = [];
    let subnet = '';

    if (subnetValue) {
      const parts = subnetValue.split('/');
      const address = new ipaddr.Address6(parts[0]);
      const prefix = Number(parts[1]);
      if (!address.isValid() || !Number.isInteger(prefix) || prefix < 1 || prefix > 128) {
        errors.push({ msg: '请输入有效的 IPv6 子网，例如 fd00::/48' });
      } else {
        subnet = address.canonicalForm() + '/' + prefix;
      }
    } else if (customAssign) {
      errors.push({ msg: '使用自定义 IPv6 地址池时必须填写 IPv6 子网' });
    }

    if (customAssign) {
      const start = new ipaddr.Address6(poolStart);
      const end = new ipaddr.Address6(poolEnd);
      if (!start.isValid() || !end.isValid()) errors.push({ msg: '自定义范围必须是有效的 IPv6 地址' });
    }

    if (errors.length) {
      return res.render('v6AssignMode', {
        title: 'IPv6 分配', navigate: navigate, network: current, errors: errors,
        form: {
          ipv6Subnet: subnetValue, poolStart: poolStart, poolEnd: poolEnd,
          customAssign: customAssign, rfc4193: toBool(req.body.rfc4193), sixplane: toBool(req.body['6plane'])
        }
      });
    }

    const pools = (current.ipAssignmentPools || []).filter(pool => !isIPv6Value(pool.ipRangeStart));
    if (customAssign) pools.push({ ipRangeStart: poolStart, ipRangeEnd: poolEnd });
    const object = {
      routes: replaceLocalRoute(current.routes, 6, subnet),
      ipAssignmentPools: pools,
      v6AssignMode: {
        '6plane': toBool(req.body['6plane']),
        'rfc4193': toBool(req.body.rfc4193),
        'zt': customAssign
      }
    };
    const network = await zt.network_object(req.params.nwid, object);
    navigate.whence = '/network/' + network.nwid;
    res.render('v6AssignMode', { title: 'IPv6 分配', navigate: navigate, network: network, message: 'IPv6 分配设置已保存' });
  } catch (err) {
    res.render('v6AssignMode', { title: 'IPv6 分配', navigate: navigate, error: '保存 IPv6 分配设置时出错 ' + req.params.nwid + ': ' + err });
  }
}

// dns POST
exports.dns = async function (req, res) {
  const navigate = {
    active: 'networks',
    whence: ''
  };

  const domain = String(req.body.domain || '').trim();
  const servers = String(req.body.servers || '')
    .split(/\r?\n/)
    .map(value => value.trim())
    .filter(Boolean);
  const invalidServers = servers.filter(ip =>
    !new ipaddr.Address4(ip).isValid() && !new ipaddr.Address6(ip).isValid()
  );

  try {
    if (invalidServers.length) {
      const network = await zt.network_detail(req.params.nwid);
      navigate.whence = '/network/' + network.nwid;
      return res.render('dns', {
        title: 'DNS 设置', navigate: navigate, network: network,
        form: { domain: domain, servers: servers },
        errors: [{ msg: '无效的 DNS 服务器地址：' + invalidServers.join('、') }]
      });
    }

    const dns = { dns: { domain: domain, servers: servers } };
    const network = await zt.network_object(req.params.nwid, dns);
    navigate.whence = '/network/' + network.nwid;
    res.render('dns', { title: 'DNS 设置', navigate: navigate, network: network, message: 'DNS 设置已保存' });
  } catch (err) {
    res.render('dns', { title: 'DNS 设置', navigate: navigate, error: '更新网络 DNS 时出错 ' + req.params.nwid + ': ' + err });
  }
}

exports.member_pin = async function (req, res) {
  const pinned = toBool(req.body.pinned);
  try {
    const pinnedMembers = await getPinnedMembers(req.params.nwid);
    const pinnedSet = new Set(pinnedMembers);
    if (pinned) pinnedSet.add(req.params.id);
    else pinnedSet.delete(req.params.id);
    await storage.setItem(pinnedMembersKey(req.params.nwid), Array.from(pinnedSet));
    res.json({ success: true, pinned: pinned });
  } catch (err) {
    console.error('保存成员置顶状态时出错:', err);
    res.status(500).json({ success: false, error: '保存置顶状态失败' });
  }
};

// Display detail page for specific member
exports.member_detail = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const { network, member } = await get_network_member(req.params.nwid, req.params.id);
    navigate.whence = '/network/' + network.nwid + '#members';
    res.render('member_detail', { title: '网络成员详细信息', navigate: navigate, network: network, member: member });
  } catch (err) {
    console.error(err);
    res.render('error', { title: req.params.object, navigate: navigate, error: '解析成员的详细信息时出错 ' + req.params.id + ' 从网络 ' + req.params.nwid + ': ' + err });
  }
};

// Member object GET
exports.member_object = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const { network, member } = await get_network_member(req.params.nwid, req.params.id);
    navigate.whence = '/network/' + network.nwid + '#members';
    res.render(req.params.object, { title: req.params.object, navigate: navigate, network: network, member: member }, function (err, html) {
      if (err) {
        if (err.message.indexOf('Failed to lookup view') !== -1) {
          return res.render('not_implemented', { title: req.params.object, navigate: navigate, network: network, member: member });
        }
        throw err;
      }
      res.send(html);
    });
  } catch (err) {
    res.render(req.params.object, { title: req.params.object, navigate: navigate, error: '解析成员的详细信息时出错 ' + req.params.id + ' 从网络 ' + req.params.nwid + ': ' + err });
  }
}

// Easy network setup GET
exports.easy_get = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/network/' + req.params.nwid
  }

  try {
    const network = await zt.network_detail(req.params.nwid);
    res.render('network_easy', { title: '网络的简易设置', navigate: navigate, network: network });
  } catch (err) {
    res.render('network_easy', { title: '网络的简易设置', navigate: navigate, error: '解析网络的详细信息时出错 ' + req.params.nwid + ': ' + err });
  }
}

// Easy network setup POST
exports.easy_post = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  req.checkBody('networkCIDR', '网络地址是必需的').notEmpty();
  req.sanitize('networkCIDR').trim();
  req.checkBody('networkCIDR', '网络地址必须使用CIDR表示法')
    .custom(value => {
      const parts = value.split('/');
      const ipv4 = new ipaddr.Address4(parts[0]);
      return ipv4.isValid() && isValidPrefix(parts[1], 32);
    });
  req.checkBody('poolStart', 'ip分配池起始地址是必需的')
    .notEmpty();
  req.checkBody('poolStart', 'ip分配池起始地址必须是有效的IPv4地址')
    .isIP(4);
  req.sanitize('poolStart').escape();
  req.sanitize('poolStart').trim();
  req.checkBody('poolEnd', 'ip分配池结束地址是必需的')
    .notEmpty();
  req.checkBody('poolEnd', 'ip分配池结束地址必须是有效的IPv4地址')
    .isIP(4);
  req.sanitize('poolEnd').escape();
  req.sanitize('poolEnd').trim();

  const errors = req.validationErrors();

  const ipAssignmentPools =
    [{
      ipRangeStart: req.body.poolStart,
      ipRangeEnd: req.body.poolEnd
    }];

  const routes =
    [{
      target: req.body.networkCIDR,
      via: null
    }];

  const v4AssignMode =
  {
    zt: true
  };

  if (errors) {
    network =
    {
      ipAssignmentPools: ipAssignmentPools,
      routes: routes,
      v4AssignMode: v4AssignMode
    };

    res.render('network_easy', { title: '网络的简易设置', navigate: navigate, network: network, errors: errors });
  } else {
    try {
      const network = await zt.network_easy_setup(req.params.nwid,
        routes,
        ipAssignmentPools,
        v4AssignMode);
      res.render('network_easy', { title: '网络的简易设置', navigate: navigate, network: network, message: 'Network setup succeeded' });
    } catch (err) {
      res.render('network_easy', { title: '网络的简易设置', navigate: navigate, error: '解析网络的详细信息时出错 ' + req.params.nwid + ': ' + err });
    }
  }
}

// Easy members auth POST
exports.members = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: '/networks'
  }

  let errors = null;

  if (req.method === 'POST') {

    req.checkBody('id', '成员ID是必需的').notEmpty();
    req.sanitize('id').trim();
    req.sanitize('id').escape();

    if (req.body.auth !== undefined) {
      req.checkBody('auth', '授权状态必须为布尔值').isBoolean();
      req.sanitize('auth').trim();
      req.sanitize('auth').escape();

      errors = req.validationErrors();

      if (!errors) {
        const auth =
        {
          authorized: req.body.auth
        };

        try {
          const mem = await zt.member_object(req.params.nwid, req.body.id, auth);
        } catch (err) {
          throw err;
        }
      }
    } else if (req.body.activeBridge !== undefined) {
      req.checkBody('activeBridge', '活动网桥状态必须为布尔值').isBoolean();
      req.sanitize('activeBridge').trim();
      req.sanitize('activeBridge').escape();

      errors = req.validationErrors();

      if (!errors) {
        const activeBridge =
        {
          activeBridge: req.body.activeBridge
        };

        try {
          const mem = await zt.member_object(req.params.nwid, req.body.id, activeBridge);
        } catch (err) {
          throw err;
        }
      }
    } else if (req.body.name) {
      req.sanitize('name').trim();
      req.sanitize('name').escape();

      errors = req.validationErrors();

      if (!errors) {
        try {
          const ret = await storage.setItem(req.body.id, req.body.name);
        } catch (err) {
          throw err;
        }
      }
    }
  } else { // GET
    res.redirect("/network/" + req.params.nwid + "#members");
  }
}

// Member delete GET or POST
exports.member_delete = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const network = await zt.network_detail(req.params.nwid);
    let member = null;
    let name = null;
    if (req.method === 'POST') {
      member = await zt.member_delete(req.params.nwid, req.params.id);
      if (member.deleted) {
        name = await storage.removeItem(member.id);
        const pinnedMembers = await getPinnedMembers(req.params.nwid);
        await storage.setItem(pinnedMembersKey(req.params.nwid), pinnedMembers.filter(id => id !== member.id));
      }
    } else {
      member = await zt.member_detail(req.params.nwid, req.params.id);
      name = await storage.getItem(member.id);
    }
    member.name = name || '';

    navigate.whence = '/network/' + network.nwid;
    res.render('member_delete', {
      title: '删除成员从 ' + network.name,
      navigate: navigate, network: network, member: member
    });
  } catch (err) {
    res.render('member_delete', {
      title: '从网络中删除成员', navigate: navigate,
      error: '解析成员的详细信息时出错 ' + req.params.id
        + ' 从网络 ' + req.params.nwid + ': ' + err
    });
  }
}

// ipAssignment delete GET
exports.delete_ip = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    const network = await zt.network_detail(req.params.nwid);
    let member = await zt.member_detail(req.params.nwid, req.params.id);
    navigate.whence = '/network/' + network.nwid;
    member.name = await storage.getItem(member.id) | '';
    if (req.params.index) {
      member = await zt.ipAssignmentDelete(network.nwid, member.id,
        req.params.index);
      res.redirect('/network/' + network.nwid + '/member/' +
        member.id + '/ipAssignments');
    }
    res.render('ipAssignments', {
      title: 'ip分配 ' + network.name,
      navigate: navigate, index: req.params.index, network: network, member: member
    });
  } catch (err) {
    res.render('ipAssignments', {
      title: 'ip分配', navigate: navigate,
      error: '解析成员的详细信息时出错 ' + req.params.id
        + ' 从网络 ' + req.params.nwid + ': ' + err
    });
  }
}

// ipAssignments POST
exports.assign_ip = async function (req, res) {
  const navigate =
  {
    active: 'networks',
    whence: ''
  }

  try {
    var network = await zt.network_detail(req.params.nwid);
  } catch (err) {
    throw err;
  }

  req.checkBody('ipAddress', '需要IP地址').notEmpty();
  req.checkBody('ipAddress', 'IP地址必须是有效的IPv4或IPv6地址').isIP();
  req.checkBody('ipAddress', 'IP地址必须位于托管路由内')
    .custom(value => {
      let ipAddressInManagedRoute = false;
      network.routes.forEach(function (item) {
        let ipv4 = new ipaddr.Address4(value);
        let target4 = new ipaddr.Address4(item.target);
        if (ipv4.isValid() && target4.isValid()) {
          if (ipv4.isInSubnet(target4)) ipAddressInManagedRoute = true;
        }
        let ipv6 = new ipaddr.Address6(value);
        let target6 = new ipaddr.Address6(item.target);
        if (ipv6.isValid() && target6.isValid()) {
          if (ipv6.isInSubnet(target6)) ipAddressInManagedRoute = true;
        }
      });
      return ipAddressInManagedRoute;
    });
  req.sanitize('ipAddress').escape();
  req.sanitize('ipAddress').trim();

  const errors = req.validationErrors();

  const ipAssignment = { ipAddress: req.body.ipAddress };

  try {
    let member = await zt.member_detail(req.params.nwid, req.params.id);
    navigate.whence = '/network/' + network.nwid;

    if (!errors) {
      member = await zt.ipAssignmentAdd(network.nwid, member.id, ipAssignment);
    }

    member.name = await storage.getItem(member.id) | '';

    res.render('ipAssignments', {
      title: 'ip分配', navigate: navigate,
      ipAssignment: ipAssignment, network: network, member: member,
      errors: errors
    });
  } catch (err) {
    res.render('ipAssignments', {
      title: 'ip分配', navigate: navigate,
      error: '解析成员的详细信息时出错 ' + req.params.id
        + ' 从网络 ' + req.params.nwid + ': ' + err
    });
  }
}
