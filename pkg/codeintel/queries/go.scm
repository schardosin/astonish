(function_declaration name: (identifier) @definition.function)
(method_declaration name: (field_identifier) @definition.method)
(type_declaration (type_spec name: (type_identifier) @definition.type))
(const_spec name: (identifier) @definition.constant)
(var_spec name: (identifier) @definition.variable)
(call_expression function: (identifier) @reference)
(call_expression function: (selector_expression field: (field_identifier) @reference))
(selector_expression field: (field_identifier) @reference)
(type_identifier) @reference
