const contracts = require("./api-contracts.json");

function validateContract(contractName, value) {
  const errors = [];
  validateValue(contractName, value, "$", errors);
  if (errors.length > 0) {
    throw new Error(`Contract ${contractName} failed:\n${errors.join("\n")}`);
  }
}

function validateValue(type, value, path, errors) {
  if (type.endsWith("[]")) {
    if (!Array.isArray(value)) {
      errors.push(`${path}: expected ${type}, got ${describe(value)}`);
      return;
    }
    const itemType = type.slice(0, -2);
    value.forEach((item, index) => validateValue(itemType, item, `${path}[${index}]`, errors));
    return;
  }

  if (type.startsWith("map:")) {
    if (!plainObject(value)) {
      errors.push(`${path}: expected ${type}, got ${describe(value)}`);
      return;
    }
    const itemType = type.slice("map:".length);
    Object.entries(value).forEach(([key, item]) => validateValue(itemType, item, `${path}.${key}`, errors));
    return;
  }

  if (contracts[type]) {
    validateObject(type, value, path, errors);
    return;
  }

  if (type === "string" && typeof value !== "string") {
    errors.push(`${path}: expected string, got ${describe(value)}`);
  } else if (type === "integer" && (!Number.isInteger(value) || !Number.isFinite(value))) {
    errors.push(`${path}: expected integer, got ${describe(value)}`);
  } else if (type === "number" && (typeof value !== "number" || !Number.isFinite(value))) {
    errors.push(`${path}: expected number, got ${describe(value)}`);
  } else if (type === "boolean" && typeof value !== "boolean") {
    errors.push(`${path}: expected boolean, got ${describe(value)}`);
  } else if (!["string", "integer", "number", "boolean"].includes(type)) {
    errors.push(`${path}: unknown contract type ${type}`);
  }
}

function validateObject(contractName, value, path, errors) {
  if (!plainObject(value)) {
    errors.push(`${path}: expected ${contractName}, got ${describe(value)}`);
    return;
  }

  const contract = contracts[contractName];
  const required = contract.required || {};
  const optional = contract.optional || {};
  const allowed = new Set([...Object.keys(required), ...Object.keys(optional)]);

  Object.keys(required).forEach((key) => {
    if (!Object.prototype.hasOwnProperty.call(value, key)) {
      errors.push(`${path}.${key}: missing required field`);
      return;
    }
    validateValue(required[key], value[key], `${path}.${key}`, errors);
  });

  Object.keys(optional).forEach((key) => {
    if (Object.prototype.hasOwnProperty.call(value, key)) {
      validateValue(optional[key], value[key], `${path}.${key}`, errors);
    }
  });

  Object.keys(value).forEach((key) => {
    if (!allowed.has(key)) {
      errors.push(`${path}.${key}: unexpected field`);
    }
  });
}

function plainObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function describe(value) {
  if (Array.isArray(value)) return "array";
  if (value === null) return "null";
  return `${typeof value} ${JSON.stringify(value)}`;
}

module.exports = { contracts, validateContract };
